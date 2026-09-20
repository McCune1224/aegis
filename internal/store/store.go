package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"aegis/db"
	"aegis/internal/client"
	"aegis/internal/filter"
	"aegis/internal/rewrite"
	"aegis/internal/store/storedb"
)

// Client is one client record with the selectors that identify it.
type Client struct {
	Key       filter.ClientKey
	Profile   filter.ProfileID
	Notes     string
	Addresses []netip.Addr
	Prefixes  []netip.Prefix
}

// Config is the stored configuration: the profiles, the client records with
// their selectors, the user-created rules, and the profile an unidentified
// client gets. The shapes the engine and the identity resolver take are
// projections of it, so a client's profile and its selectors cannot drift
// apart.
type Config struct {
	Profiles  []filter.ProfileSpec
	Clients   []Client
	Rules     []filter.RuleSpec
	Schedules []filter.ScheduleSpec
	Rewrites  []rewrite.Record
	Upstreams []Upstream
	Routes    []Route
	// Services is the fetched catalog of blockable online services, and
	// ProfileServices is which profiles block which of them.
	Services        []BlockedService
	ProfileServices []ProfileService
	// Allowed and Disallowed gate the listener itself: a disallowed client is
	// refused before any processing, and a non-empty allowed set makes the
	// listener serve only the clients it lists.
	Allowed    []netip.Prefix
	Disallowed []netip.Prefix
	Default    filter.ProfileID
}

// ClientSpecs is the client list in the shape filter.Compile takes.
func (c Config) ClientSpecs() []filter.ClientSpec {
	specs := make([]filter.ClientSpec, 0, len(c.Clients))
	for _, record := range c.Clients {
		specs = append(specs, filter.ClientSpec{Key: record.Key, Profile: record.Profile})
	}
	return specs
}

// Selectors is the client list in the shape client.New takes.
func (c Config) Selectors() []client.Spec {
	specs := make([]client.Spec, 0, len(c.Clients))
	for _, record := range c.Clients {
		specs = append(specs, client.Spec{Key: record.Key, Addresses: record.Addresses, Prefixes: record.Prefixes})
	}
	return specs
}

// Validate compiles the configuration, the same check the runtime makes on
// every reload. A change that would fail to load is refused before it reaches
// the database.
func (c Config) Validate() error {
	if _, err := client.New(c.Selectors()); err != nil {
		return err
	}
	if err := validateAccess(c.Allowed, c.Disallowed); err != nil {
		return err
	}
	if err := validateProfileServices(c.ProfileServices, c.Services, c.Profiles); err != nil {
		return err
	}
	if _, err := filter.Compile(filter.Config{
		Rules:     c.Rules,
		Profiles:  c.Profiles,
		Clients:   c.ClientSpecs(),
		Schedules: c.Schedules,
		Default:   c.Default,
	}); err != nil {
		return err
	}
	return validateRoutes(c.Routes, c.Upstreams, c.Clients)
}

// validateRoutes refuses a route whose upstream is missing or disabled,
// whose client does not exist, or whose domain no query can carry. A route
// that fails here would fail every reload, because the router has nowhere to
// send the queries it matches.
func validateRoutes(routes []Route, upstreams []Upstream, clients []Client) error {
	enabled := make(map[string]bool, len(upstreams))
	for _, row := range upstreams {
		if row.Enabled {
			enabled[row.Name] = true
		}
	}
	known := make(map[filter.ClientKey]bool, len(clients))
	for _, record := range clients {
		known[record.Key] = true
	}
	for _, route := range routes {
		if !enabled[route.Upstream] {
			return fmt.Errorf("store: route %d names upstream %q, which is not an enabled resolver", route.ID, route.Upstream)
		}
		if route.Client != "" && !known[filter.ClientKey(route.Client)] {
			return fmt.Errorf("store: route %d names client %q, which does not exist", route.ID, route.Client)
		}
		if route.Domain != "" {
			if _, err := filter.ParseDomain(route.Domain); err != nil {
				return fmt.Errorf("store: route %d: %w", route.ID, err)
			}
		}
	}
	return nil
}

// Store is the configuration Aegis serves from. It owns two handles to the
// same database: one for configuration, whose writes stay serialized so SQLite
// never returns SQLITE_BUSY under a burst of administrative writes, and one
// for the query log, whose recurring flushes must never queue configuration
// work behind them or vice versa. WAL mode lets the log's reads and writes
// overlap inside the log handle.
type Store struct {
	db      *sql.DB
	logDB   *sql.DB
	queries *storedb.Queries
	logQuer *storedb.Queries
}

// Open opens the database at path and applies every pending migration.
func Open(ctx context.Context, path string) (*Store, error) {
	// WAL lets reads run beside a write, and busy_timeout absorbs the moment
	// two writes collide.
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	sqlDB.SetMaxOpenConns(1)

	logDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	logDB.SetMaxOpenConns(2)

	if err := migrate(ctx, sqlDB); err != nil {
		_ = sqlDB.Close()
		_ = logDB.Close()
		return nil, err
	}
	return &Store{db: sqlDB, logDB: logDB, queries: storedb.New(sqlDB), logQuer: storedb.New(logDB)}, nil
}

func migrate(ctx context.Context, sqlDB *sql.DB) error {
	migrations, err := fs.Sub(db.Migrations, "migrations")
	if err != nil {
		return fmt.Errorf("store: migrations: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, sqlDB, migrations)
	if err != nil {
		return fmt.Errorf("store: migrations: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}

// Close releases both database handles.
func (s *Store) Close() error {
	return errors.Join(s.db.Close(), s.logDB.Close())
}

// Load reads the configuration, parsing every text column into the typed value
// the layers above take. Nothing above this point sees a string where it should
// see a blocking mode or an address.
func (s *Store) Load(ctx context.Context) (Config, error) {
	defaultProfile, err := s.queries.GetSetting(ctx, "default_profile")
	if err != nil {
		return Config{}, fmt.Errorf("store: default profile: %w", err)
	}

	profiles, err := s.loadProfiles(ctx)
	if err != nil {
		return Config{}, err
	}

	clients, err := s.loadClients(ctx)
	if err != nil {
		return Config{}, err
	}

	rules, err := s.loadRules(ctx)
	if err != nil {
		return Config{}, err
	}

	specs := make([]filter.RuleSpec, 0, len(rules))
	for _, rule := range rules {
		specs = append(specs, rule.Spec())
	}

	schedules, err := s.Schedules(ctx)
	if err != nil {
		return Config{}, err
	}

	rewrites, err := s.Rewrites(ctx)
	if err != nil {
		return Config{}, err
	}

	upstreams, err := s.Upstreams(ctx)
	if err != nil {
		return Config{}, err
	}

	routes, err := s.Routes(ctx)
	if err != nil {
		return Config{}, err
	}

	allowed, disallowed, err := loadAccess(ctx, s.queries)
	if err != nil {
		return Config{}, err
	}

	catalog, err := s.Services(ctx)
	if err != nil {
		return Config{}, err
	}

	enables, err := s.ProfileServices(ctx)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Profiles:        profiles,
		Clients:         clients,
		Rules:           specs,
		Schedules:       schedules,
		Rewrites:        rewrites,
		Upstreams:       upstreams,
		Routes:          routes,
		Services:        catalog,
		ProfileServices: enables,
		Allowed:         allowed,
		Disallowed:      disallowed,
		Default:         filter.ProfileID(defaultProfile),
	}, nil
}

func (s *Store) loadClients(ctx context.Context) ([]Client, error) {
	rows, err := s.queries.ListClients(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: clients: %w", err)
	}
	addresses, err := s.loadAddresses(ctx)
	if err != nil {
		return nil, err
	}
	prefixes, err := s.loadPrefixes(ctx)
	if err != nil {
		return nil, err
	}

	clients := make([]Client, 0, len(rows))
	for _, row := range rows {
		key := filter.ClientKey(row.Name)
		clients = append(clients, Client{
			Key:       key,
			Profile:   filter.ProfileID(row.Profile),
			Notes:     row.Notes,
			Addresses: addresses[key],
			Prefixes:  prefixes[key],
		})
	}
	return clients, nil
}

func (s *Store) loadProfiles(ctx context.Context) ([]filter.ProfileSpec, error) {
	rows, err := s.queries.ListProfiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: profiles: %w", err)
	}

	profiles := make([]filter.ProfileSpec, 0, len(rows))
	for _, row := range rows {
		spec := filter.ProfileSpec{ID: filter.ProfileID(row.Name)}
		if row.Extends != nil {
			spec.Extends = filter.ProfileID(*row.Extends)
		}
		if row.Mode != nil {
			mode, err := filter.ParseBlockingMode(*row.Mode)
			if err != nil {
				return nil, fmt.Errorf("store: profile %q: %w", row.Name, err)
			}
			spec.Mode = &mode
		}
		if row.Custom != nil {
			address, err := netip.ParseAddr(*row.Custom)
			if err != nil {
				return nil, fmt.Errorf("store: profile %q: %w", row.Name, err)
			}
			spec.Custom = &address
		}
		profiles = append(profiles, spec)
	}
	return profiles, nil
}

func (s *Store) loadAddresses(ctx context.Context) (map[filter.ClientKey][]netip.Addr, error) {
	rows, err := s.queries.ListClientAddresses(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: client addresses: %w", err)
	}

	byClient := make(map[filter.ClientKey][]netip.Addr, len(rows))
	for _, row := range rows {
		address, err := netip.ParseAddr(row.Address)
		if err != nil {
			return nil, fmt.Errorf("store: client %q: %w", row.Client, err)
		}
		key := filter.ClientKey(row.Client)
		byClient[key] = append(byClient[key], address)
	}
	return byClient, nil
}

func (s *Store) loadPrefixes(ctx context.Context) (map[filter.ClientKey][]netip.Prefix, error) {
	rows, err := s.queries.ListClientPrefixes(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: client prefixes: %w", err)
	}

	byClient := make(map[filter.ClientKey][]netip.Prefix, len(rows))
	for _, row := range rows {
		prefix, err := netip.ParsePrefix(row.Prefix)
		if err != nil {
			return nil, fmt.Errorf("store: client %q: %w", row.Client, err)
		}
		key := filter.ClientKey(row.Client)
		byClient[key] = append(byClient[key], prefix)
	}
	return byClient, nil
}

// FirstBoot reports whether the stored configuration has ever been seeded from
// the flags. It reads an explicit marker rather than row counts, so an edit that
// leaves one profile and no clients is not mistaken for an empty database.
func (s *Store) FirstBoot(ctx context.Context) (bool, error) {
	_, err := s.queries.GetSetting(ctx, "seeded")
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: seeded marker: %w", err)
	}
	return false, nil
}

// MarkSeeded records that the flags have been applied, so a later boot leaves
// the database alone.
func (s *Store) MarkSeeded(ctx context.Context) error {
	if err := s.queries.SetSetting(ctx, storedb.SetSettingParams{Key: "seeded", Value: "1"}); err != nil {
		return fmt.Errorf("store: seeded marker: %w", err)
	}
	return nil
}

// SaveProfile inserts or replaces one profile.
func (s *Store) SaveProfile(ctx context.Context, spec filter.ProfileSpec) error {
	params := storedb.UpsertProfileParams{Name: string(spec.ID)}
	if spec.Extends != "" {
		extends := string(spec.Extends)
		params.Extends = &extends
	}
	if spec.Mode != nil {
		mode := spec.Mode.String()
		params.Mode = &mode
	}
	if spec.Custom != nil {
		custom := spec.Custom.String()
		params.Custom = &custom
	}
	if err := s.queries.UpsertProfile(ctx, params); err != nil {
		return fmt.Errorf("store: save profile %q: %w", spec.ID, err)
	}
	return nil
}

// SaveClient inserts or replaces one client and replaces its selectors, so the
// stored selectors always match the record that was written.
func (s *Store) SaveClient(ctx context.Context, record Client) error {
	return s.inTx(ctx, func(q *storedb.Queries) error {
		params := storedb.UpsertClientParams{
			Name:    string(record.Key),
			Profile: string(record.Profile),
			Notes:   record.Notes,
		}
		if err := q.UpsertClient(ctx, params); err != nil {
			return fmt.Errorf("store: save client %q: %w", record.Key, err)
		}

		name := string(record.Key)
		if err := q.DeleteClientAddressesForClient(ctx, name); err != nil {
			return fmt.Errorf("store: save client %q: %w", record.Key, err)
		}
		for _, address := range record.Addresses {
			if err := q.UpsertClientAddress(ctx, storedb.UpsertClientAddressParams{
				Address: address.Unmap().String(),
				Client:  name,
			}); err != nil {
				return fmt.Errorf("store: save client %q: %w", record.Key, err)
			}
		}

		if err := q.DeleteClientPrefixesForClient(ctx, name); err != nil {
			return fmt.Errorf("store: save client %q: %w", record.Key, err)
		}
		for _, prefix := range record.Prefixes {
			if err := q.UpsertClientPrefix(ctx, storedb.UpsertClientPrefixParams{
				Prefix: prefix.Masked().String(),
				Client: name,
			}); err != nil {
				return fmt.Errorf("store: save client %q: %w", record.Key, err)
			}
		}
		return nil
	})
}

// SetDefaultProfile names the profile an unidentified client gets.
func (s *Store) SetDefaultProfile(ctx context.Context, profile filter.ProfileID) error {
	if err := s.queries.SetSetting(ctx, storedb.SetSettingParams{Key: "default_profile", Value: string(profile)}); err != nil {
		return fmt.Errorf("store: default profile: %w", err)
	}
	return nil
}

// DeleteProfile removes one profile. A caller validates the resulting
// configuration first, because a profile another profile extends or a client
// names must be changed before this one can go.
func (s *Store) DeleteProfile(ctx context.Context, id filter.ProfileID) error {
	if err := s.queries.DeleteProfile(ctx, string(id)); err != nil {
		return fmt.Errorf("store: delete profile %q: %w", id, err)
	}
	return nil
}

// DeleteClient removes one client. Its selectors go with it through the
// foreign key's cascade.
func (s *Store) DeleteClient(ctx context.Context, key filter.ClientKey) error {
	if err := s.queries.DeleteClient(ctx, string(key)); err != nil {
		return fmt.Errorf("store: delete client %q: %w", key, err)
	}
	return nil
}

func (s *Store) inTx(ctx context.Context, fn func(*storedb.Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if err := fn(s.queries.WithTx(tx)); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// inLogTx runs one transaction against the query log's handle, so a log batch
// commits on its own connection and never holds the configuration handle.
func (s *Store) inLogTx(ctx context.Context, fn func(*storedb.Queries) error) error {
	tx, err := s.logDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: log tx: %w", err)
	}
	if err := fn(s.logQuer.WithTx(tx)); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
