package store

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/netip"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"aegis/db"
	"aegis/internal/client"
	"aegis/internal/filter"
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

// Config is the stored configuration in the shape the engine and the identity
// resolver take.
type Config struct {
	Profiles  []filter.ProfileSpec
	Clients   []filter.ClientSpec
	Selectors []client.Spec
	Default   filter.ProfileID
}

// Store is the configuration Aegis serves from. It owns the database handle and
// is the only place a text column becomes a typed value.
type Store struct {
	db      *sql.DB
	queries *storedb.Queries
}

// Open opens the database at path and applies every pending migration.
func Open(ctx context.Context, path string) (*Store, error) {
	// One connection serializes writers, which keeps SQLite from returning
	// SQLITE_BUSY under a burst of configuration writes. The DNS path never
	// touches the database, so this only queues administrative work.
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := migrate(ctx, sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return &Store{db: sqlDB, queries: storedb.New(sqlDB)}, nil
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

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

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

	rows, err := s.queries.ListClients(ctx)
	if err != nil {
		return Config{}, fmt.Errorf("store: clients: %w", err)
	}
	addresses, err := s.loadAddresses(ctx)
	if err != nil {
		return Config{}, err
	}
	prefixes, err := s.loadPrefixes(ctx)
	if err != nil {
		return Config{}, err
	}

	clients := make([]filter.ClientSpec, 0, len(rows))
	selectors := make([]client.Spec, 0, len(rows))
	for _, row := range rows {
		key := filter.ClientKey(row.Name)
		clients = append(clients, filter.ClientSpec{Key: key, Profile: filter.ProfileID(row.Profile)})
		selectors = append(selectors, client.Spec{
			Key:       key,
			Addresses: addresses[key],
			Prefixes:  prefixes[key],
		})
	}

	return Config{
		Profiles:  profiles,
		Clients:   clients,
		Selectors: selectors,
		Default:   filter.ProfileID(defaultProfile),
	}, nil
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

// Unconfigured reports whether nothing beyond the migration's default profile
// has been stored. The serve command uses it to decide whether to seed from its
// flags on first boot.
func (s *Store) Unconfigured(ctx context.Context) (bool, error) {
	profiles, err := s.queries.ListProfiles(ctx)
	if err != nil {
		return false, fmt.Errorf("store: profiles: %w", err)
	}
	clients, err := s.queries.ListClients(ctx)
	if err != nil {
		return false, fmt.Errorf("store: clients: %w", err)
	}
	return len(profiles) == 1 && len(clients) == 0, nil
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
