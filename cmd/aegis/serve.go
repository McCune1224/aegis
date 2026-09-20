package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"aegis/internal/api"
	"aegis/internal/blocklist"
	"aegis/internal/cache"
	"aegis/internal/client"
	"aegis/internal/config"
	"aegis/internal/dhcp"
	"aegis/internal/dns"
	"aegis/internal/filter"
	"aegis/internal/metrics"
	"aegis/internal/querylog"
	"aegis/internal/ratelimit"
	"aegis/internal/rewrite"
	"aegis/internal/runtime"
	"aegis/internal/services"
	"aegis/internal/store"
	"aegis/internal/threat"
	"aegis/internal/upstream"
	"aegis/web"
)

// defaultProfile is the profile every client gets until a client entry names
// another one.
const defaultProfile filter.ProfileID = "default"

// sourceTimeout bounds one blocklist fetch, redirects included.
const sourceTimeout = 30 * time.Second

// sourceRefreshTick is how often the background loop looks for sources whose
// refresh schedule has elapsed.
const sourceRefreshTick = time.Minute

// catalogRefreshInterval is how often the blocked-services catalog is fetched.
// The sets change between releases, so a daily fetch keeps them current
// without an operator action.
const catalogRefreshInterval = 24 * time.Hour

func newServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the DNS server",
		Args:  cobra.NoArgs,
		RunE:  runServe,
	}

	flags := cmd.Flags()
	flags.String("dns-address", "127.0.0.1:53", "address to listen on, as host:port")
	flags.String("dhcp-address", "", "address to serve DHCPv4 on, as host:port, usually 0.0.0.0:67 because clients broadcast, enables the DHCP server")
	flags.String("dhcp-range", "", "addresses the DHCP server hands out, as first-last, required with dhcp-address")
	flags.String("dhcp-netmask", "255.255.255.0", "netmask of the network the DHCP pool belongs to")
	flags.String("dhcp-server-ip", "", "address clients renew with, defaults to the first address of the pool network")
	flags.String("dhcp-router", "", "address clients are told to route through, defaults to dhcp-server-ip")
	flags.StringArray("dhcp-dns", nil, "resolver clients are told to use, repeatable, defaults to the DNS listener address")
	flags.String("dhcp-lease-time", "12h", "how long a DHCP lease lasts")
	flags.String("dot-address", "", "address to serve DNS over TLS on, as host:port, requires tls-cert and tls-key")
	flags.String("doh-address", "", "address to serve DNS over HTTPS on, as host:port, requires tls-cert and tls-key")
	flags.String("tls-cert", "", "path to the PEM certificate chain the encrypted listeners serve")
	flags.String("tls-key", "", "path to the PEM private key the encrypted listeners serve")
	flags.StringArray("upstream", []string{"9.9.9.9:53"}, "upstream resolver as a URL: udp://, tcp://, tls://, or https://, repeatable")
	flags.String("api-address", defaultAPIAddress, "address for the HTTP API, as host:port")
	flags.String("db", "aegis.db", "path to the configuration database")
	flags.String("blocking-mode", "nxdomain", "nxdomain, null-address, custom-address, or refused, for the default profile")
	flags.String("custom-address", "", "the address to answer with when the default profile blocks")
	flags.StringArray("blocklist", nil, "path to a blocklist file, repeatable, as [format:]path with format hosts, domains, or adblock")
	flags.StringArray("source", nil, "blocklist source as name=[format:]url, repeatable")
	flags.StringArray("threat-feed", nil, "threat feed as name=url, repeatable")
	flags.String("block-format", "hosts", "hosts, domains, or adblock, applied to every blocklist")
	flags.StringArray("profile", nil, "extra profile as name=mode or name=mode=address, repeatable")
	flags.StringArray("client", nil, "bind an address to a profile as address=profile, repeatable")
	flags.StringArray("rewrite", nil, "rewrite a name as pattern=target, where target is an address or another name, repeatable")
	flags.String("source-refresh", "6h", "how often to fetch blocklist sources for updates, as a duration")
	flags.Float64("rate-limit", 0, "queries per second one client may ask, 0 disables rate limiting")
	flags.Int("rate-burst", 0, "queries one client may ask in an instant, requires rate-limit")
	flags.String("log-level", "info", "debug, info, warn, or error")

	return cmd
}

func runServe(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	cfg, err := config.Load(cmd.Flags())
	if err != nil {
		return err
	}

	logger, err := newLogger(cfg.LogLevel, cmd.ErrOrStderr())
	if err != nil {
		return err
	}

	// Everything the flags describe is parsed before the database is touched, so
	// a typo in a flag cannot leave a half-initialised file behind.
	format, err := blocklist.ParseFormat(cfg.BlockFormat)
	if err != nil {
		return err
	}
	profiles, err := buildProfiles(cfg)
	if err != nil {
		return err
	}
	records, err := buildClients(cfg.Clients)
	if err != nil {
		return err
	}
	if err := validateUpstreams(cfg.Upstreams); err != nil {
		return err
	}
	rewrites, err := buildRewrites(cfg.Rewrites)
	if err != nil {
		return err
	}
	tlsConfig, err := buildTLSConfig(cfg.TLSCert, cfg.TLSKey, cfg.DoTAddress, cfg.DoHAddress)
	if err != nil {
		return err
	}

	lists := make([]runtime.ListFile, 0, len(cfg.Blocklists))
	for _, path := range cfg.Blocklists {
		lists = append(lists, parseBlocklistEntry(path, format))
	}
	if err := runtime.ValidateLists(lists); err != nil {
		return err
	}

	database, err := store.Open(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	firstBoot, err := database.FirstBoot(ctx)
	if err != nil {
		return err
	}
	if err := seed(ctx, database, profiles, records, firstBoot, logger); err != nil {
		return err
	}
	if err := seedSources(ctx, database, cfg.Sources, format, firstBoot, logger); err != nil {
		return err
	}
	if err := seedThreatFeeds(ctx, database, cfg.ThreatFeeds, firstBoot, logger); err != nil {
		return err
	}
	if err := seedRewrites(ctx, database, rewrites, firstBoot, logger); err != nil {
		return err
	}
	if err := seedUpstreams(ctx, database, cfg.Upstreams, logger); err != nil {
		return err
	}
	if err := database.MarkSeeded(ctx); err != nil {
		return err
	}
	dhcpConfig, err := buildDHCPConfig(cfg)
	if err != nil {
		_ = database.Close()
		return err
	}

	counts := metrics.New()
	leases := client.NewDynamic()
	engine := runtime.New(database, lists, logger, runtime.WithMetrics(counts), runtime.WithLeases(leases))
	sync := runtime.NewSourceSync(database, blocklist.NewFetcher(sourceTimeout), engine, logger)
	if err := sync.RefreshSources(ctx); err != nil {
		return err
	}
	catalog := runtime.NewServiceSync(database, blocklist.NewFetcher(sourceTimeout), services.DefaultCatalogURL, engine, logger)

	hub := api.NewHub(logger)
	threats := threat.NewService(database, logger)
	log := querylog.New(database, logger, querylog.WithThreats(threats.ThreatFor))
	threatSync := runtime.NewThreatSync(database, blocklist.NewFetcher(sourceTimeout), threats, logger)
	if err := threatSync.RefreshFeeds(ctx); err != nil {
		return err
	}

	resolver, err := cache.New(cache.Config{Upstream: engine.Upstreams(), Prefetch: true})
	if err != nil {
		return err
	}
	counts.WatchDrops(hub.Dropped)
	counts.WatchCache(func() metrics.CacheCounters {
		stats := resolver.Stats()
		return metrics.CacheCounters{
			Hits:       stats.Hits,
			Misses:     stats.Misses,
			Evictions:  stats.Evictions,
			Prefetches: stats.Prefetches,
		}
	})
	counts.WatchUpstreams(func() []metrics.UpstreamStat {
		stats := engine.Upstreams().Stats()
		health := make([]metrics.UpstreamStat, 0, len(stats))
		for _, stat := range stats {
			health = append(health, metrics.UpstreamStat{Name: stat.Name, EWMA: stat.EWMA, Failures: stat.Failures, Down: stat.Down})
		}
		return health
	})
	limiter, err := buildLimiter(cfg, engine)
	if err != nil {
		return err
	}
	handlerConfig := dns.Config{
		Decider:   engine,
		Upstream:  resolver,
		Rewriter:  engine,
		Gate:      engine,
		Observers: []dns.Observer{hub, log, verdictCounter{counts}, threats},
	}
	// A nil *ratelimit.Limiter inside the interface would look non-nil to the
	// handler, so the field stays unset when limiting is off.
	if limiter != nil {
		handlerConfig.Limiter = limiter
	}
	handler, err := dns.NewHandler(handlerConfig)
	if err != nil {
		return err
	}

	server, err := dns.Start(dns.ServerConfig{
		Handler:    handler,
		Address:    cfg.DNSAddress,
		DoTAddress: cfg.DoTAddress,
		DoHAddress: cfg.DoHAddress,
		TLS:        tlsConfig,
		Logger:     logger,
	})
	if err != nil {
		return err
	}

	rows, err := database.Upstreams(ctx)
	if err != nil {
		return err
	}

	apiServer, err := api.Start(api.Config{
		Store:     database,
		Reloader:  engine,
		Sources:   sync,
		Preview:   sync,
		Catalog:   catalog,
		Threats:   threatSync,
		Hub:       hub,
		Files:     web.Files(),
		Address:   cfg.APIAddress,
		Logger:    logger,
		Metrics:   counts,
		Upstreams: engine.Upstreams(),
	})
	if err != nil {
		_ = server.Shutdown(context.Background())
		return err
	}

	var dhcpServer *dhcp.Server
	dhcpGroup := slog.Group("dhcp", "off", true)
	if dhcpConfig.Pool.First.IsValid() {
		dhcpConfig.Identity = engine.Select
		dhcpServer = dhcp.New(dhcpConfig, database, leases, logger)
		if err := dhcpServer.Load(ctx); err != nil {
			_ = apiServer.Shutdown(context.Background())
			_ = server.Shutdown(context.Background())
			return err
		}
		dhcpGroup = slog.Group("dhcp",
			"listen", dhcpConfig.Address,
			"pool", dhcpConfig.Pool,
			"gateway", dhcpConfig.Router,
			"server", dhcpConfig.ServerIP,
		)
	}

	logger.Info("aegis is serving",
		"udp", server.UDPAddr().String(),
		"tcp", server.TCPAddr().String(),
		"api", apiServer.Addr().String(),
		"upstreams", strings.Join(upstreamNames(rows), ", "),
		"database", cfg.DB,
		"rules", engine.Size(),
		slog.Group("encrypted",
			"dot", listenerAddrOrOff(server.DoTAddr()),
			"doh", listenerAddrOrOff(server.DoHAddr()),
		),
		dhcpGroup,
	)

	stop, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	refreshInterval, err := time.ParseDuration(cfg.SourceRefresh)
	if err != nil {
		return fmt.Errorf("source-refresh: %w", err)
	}
	refreshCtx, refreshCancel := context.WithCancel(stop)
	defer refreshCancel()
	go runSourceRefreshLoop(refreshCtx, sync, refreshInterval, logger)
	go runCatalogRefreshLoop(refreshCtx, catalog, logger)
	go runThreatRefreshLoop(refreshCtx, threatSync, logger)

	dhcpDone := make(chan error, 1)
	if dhcpServer != nil {
		go func() { dhcpDone <- dhcpServer.Serve(stop) }()
	}

	<-stop.Done()

	logger.Info("aegis is shutting down")
	if err := log.Close(); err != nil {
		logger.Warn("querylog: final flush failed", "error", err)
	}
	if err := threats.Close(); err != nil {
		logger.Warn("threat: final flush failed", "error", err)
	}
	shutdown, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	var dhcpErr error
	if dhcpServer != nil {
		dhcpErr = <-dhcpDone
	}
	return errors.Join(apiServer.Shutdown(shutdown), server.Shutdown(shutdown), dhcpErr)
}

// validateUpstreams parses every --upstream flag before the database is
// touched, so a bad URL fails the start with the entry named.
func validateUpstreams(raw []string) error {
	if len(raw) == 0 {
		return errors.New("at least one --upstream is required")
	}
	for _, entry := range raw {
		if _, err := upstream.Parse(entry); err != nil {
			return err
		}
	}
	return nil
}

// upstreamNames lists the stored rows as written, for status and logs.
func upstreamNames(rows []store.Upstream) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Name)
	}
	return names
}

// seedUpstreams stores the flag resolvers while the table has no rows. The
// check is the empty table rather than first boot, so a database upgraded from
// a flag-only release gets its resolvers too.
func seedUpstreams(ctx context.Context, database *store.Store, flags []string, logger *slog.Logger) error {
	rows, err := database.Upstreams(ctx)
	if err != nil {
		return err
	}
	if len(rows) > 0 {
		return nil
	}
	for _, raw := range flags {
		if err := database.SaveUpstream(ctx, store.Upstream{Name: raw, URL: raw, Enabled: true}); err != nil {
			return err
		}
	}
	logger.Info("seeded upstream resolvers from the flags", "count", len(flags))
	return nil
}

// buildLimiter reads the rate flags into a limiter whose buckets key on the
// runtime's identity table, so a query and its rate bucket always agree on who
// is asking. A zero rate leaves the limiter off.
func buildLimiter(cfg config.Config, engine *runtime.Runtime) (*ratelimit.Limiter, error) {
	if cfg.RateLimit == 0 {
		if cfg.RateBurst != 0 {
			return nil, errors.New("rate-burst needs rate-limit to be set")
		}
		return nil, nil
	}
	if cfg.RateBurst < 1 {
		return nil, errors.New("rate-limit needs rate-burst of at least one")
	}
	return ratelimit.New(ratelimit.Config{
		Keys:  engine.ClientKey,
		Rate:  cfg.RateLimit,
		Burst: cfg.RateBurst,
	})
}

// runSourceRefreshLoop fetches every enabled source whose own schedule has
// elapsed, checking once a minute. A source without an override uses the
// configured default interval, and the ETag makes a check cheap.
func runSourceRefreshLoop(ctx context.Context, sync *runtime.SourceSync, defaultInterval time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(sourceRefreshTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fetched, err := sync.RefreshDue(ctx, time.Now(), defaultInterval)
			if err != nil {
				logger.Warn("source refresh failed", "error", err)
				continue
			}
			if fetched > 0 {
				logger.Info("refreshed blocklist sources", "count", fetched)
			}
		}
	}
}

// runCatalogRefreshLoop fetches the services catalog once at boot and then on
// an interval. It runs beside the server rather than before it, because
// blocked services are optional: a catalog that cannot be reached must not
// stop the resolver from serving, and the stored copy keeps working.
func runCatalogRefreshLoop(ctx context.Context, sync *runtime.ServiceSync, logger *slog.Logger) {
	refresh := func() {
		if err := sync.RefreshCatalog(ctx); err != nil {
			logger.Warn("services catalog refresh failed", "error", err)
		}
	}
	refresh()

	ticker := time.NewTicker(catalogRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refresh()
		}
	}
}

// seed writes the flag configuration into an empty database. Once anything is
// stored, the database is the source of truth and the flags are ignored, which
// is what lets the web app take over without a flag overwriting it on restart.
func seed(ctx context.Context, database *store.Store, profiles []filter.ProfileSpec, records []store.Client, firstBoot bool, logger *slog.Logger) error {
	if !firstBoot {
		return nil
	}

	for _, profile := range profiles {
		if err := database.SaveProfile(ctx, profile); err != nil {
			return err
		}
	}
	for _, record := range records {
		if err := database.SaveClient(ctx, record); err != nil {
			return err
		}
	}
	if err := database.SetDefaultProfile(ctx, defaultProfile); err != nil {
		return err
	}

	logger.Info("seeded an empty database from the flags",
		"profiles", len(profiles),
		"clients", len(records),
	)
	return nil
}

// parseBlocklistEntry turns one --blocklist entry into the typed file. An
// entry may name its format with an exact prefix — adblock:/path/list.txt —
// and otherwise takes the global --block-format. A prefix only counts when it
// is a format name, so drive-letter paths pass through untouched.
func parseBlocklistEntry(raw string, fallback blocklist.Format) runtime.ListFile {
	if prefix, rest, found := strings.Cut(raw, ":"); found {
		if parsed, err := blocklist.ParseFormat(prefix); err == nil {
			return runtime.ListFile{Path: rest, Format: parsed}
		}
	}
	return runtime.ListFile{Path: raw, Format: fallback}
}

// seedSources stores the flag sources on first boot, so the database stays the
// source of truth after that.
func seedSources(ctx context.Context, database *store.Store, entries []string, format blocklist.Format, firstBoot bool, logger *slog.Logger) error {
	if !firstBoot || len(entries) == 0 {
		return nil
	}
	for _, raw := range entries {
		source, err := parseSource(raw, format)
		if err != nil {
			return err
		}
		if err := database.SaveSource(ctx, source); err != nil {
			return err
		}
	}
	logger.Info("seeded blocklist sources from the flags", "sources", len(entries))
	return nil
}

// buildRewrites parses every configured rewrite before the database is
// touched, so a bad pair fails the start with the entry named, seeded or not.
func buildRewrites(entries []string) ([]rewrite.Record, error) {
	records := make([]rewrite.Record, 0, len(entries))
	for _, raw := range entries {
		pattern, target, found := strings.Cut(raw, "=")
		if !found {
			return nil, fmt.Errorf("rewrite %q must be pattern=target", raw)
		}
		record, err := rewrite.Parse(pattern, target)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

// seedRewrites stores the flag rewrites on first boot, so the database stays
// the source of truth after that.
func seedRewrites(ctx context.Context, database *store.Store, records []rewrite.Record, firstBoot bool, logger *slog.Logger) error {
	if !firstBoot || len(records) == 0 {
		return nil
	}
	for _, record := range records {
		if err := database.SaveRewrite(ctx, record); err != nil {
			return err
		}
	}
	logger.Info("seeded rewrites from the flags", "rewrites", len(records))
	return nil
}

func parseSource(raw string, format blocklist.Format) (store.Source, error) {
	name, rest, found := strings.Cut(raw, "=")
	if !found || name == "" || rest == "" {
		return store.Source{}, fmt.Errorf("source %q must be name=url", raw)
	}
	if prefix, url, found := strings.Cut(rest, ":"); found {
		if parsed, err := blocklist.ParseFormat(prefix); err == nil {
			return store.Source{Name: name, URL: url, Format: parsed, Enabled: true}, nil
		}
	}
	return store.Source{Name: name, URL: rest, Format: format, Enabled: true}, nil
}

// buildProfiles turns the default settings and any configured profiles into the
// specs the store holds.
func buildProfiles(cfg config.Config) ([]filter.ProfileSpec, error) {
	mode, err := filter.ParseBlockingMode(cfg.BlockingMode)
	if err != nil {
		return nil, err
	}
	custom, err := parseOptionalAddress(cfg.CustomAddress, "custom-address")
	if err != nil {
		return nil, err
	}

	profiles := []filter.ProfileSpec{{
		ID:     defaultProfile,
		Mode:   &mode,
		Custom: customPointer(custom),
	}}
	for _, raw := range cfg.Profiles {
		profile, err := parseProfile(raw)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

// parseProfile reads name=mode or name=mode=address. The separator is = rather
// than a colon because an IPv6 address carries colons of its own.
func parseProfile(raw string) (filter.ProfileSpec, error) {
	parts := strings.Split(raw, "=")
	if len(parts) < 2 || len(parts) > 3 || parts[0] == "" {
		return filter.ProfileSpec{}, fmt.Errorf("profile %q must be name=mode or name=mode=address", raw)
	}

	mode, err := filter.ParseBlockingMode(parts[1])
	if err != nil {
		return filter.ProfileSpec{}, err
	}
	profile := filter.ProfileSpec{ID: filter.ProfileID(parts[0]), Mode: &mode}

	if len(parts) == 3 {
		address, err := netip.ParseAddr(parts[2])
		if err != nil {
			return filter.ProfileSpec{}, fmt.Errorf("profile %q: %w", raw, err)
		}
		profile.Custom = &address
	}
	return profile, nil
}

// buildClients reads address=profile. The address doubles as the client key
// until the web app gives clients names.
func buildClients(entries []string) ([]store.Client, error) {
	var records []store.Client
	for _, raw := range entries {
		addressText, profile, found := strings.Cut(raw, "=")
		if !found || profile == "" {
			return nil, fmt.Errorf("client %q must be address=profile", raw)
		}
		address, err := netip.ParseAddr(addressText)
		if err != nil {
			return nil, fmt.Errorf("client %q: %w", raw, err)
		}

		records = append(records, store.Client{
			Key:       filter.ClientKey(address.String()),
			Profile:   filter.ProfileID(profile),
			Addresses: []netip.Addr{address},
		})
	}
	return records, nil
}

func parseOptionalAddress(raw, field string) (netip.Addr, error) {
	if strings.TrimSpace(raw) == "" {
		return netip.Addr{}, nil
	}
	address, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("%s: %w", field, err)
	}
	return address, nil
}

func customPointer(address netip.Addr) *netip.Addr {
	if !address.IsValid() {
		return nil
	}
	return &address
}

// verdictCounter adapts DNS decisions to the metrics counters. Observe runs
// on the DNS worker, so it must stay lock-free — CountVerdict only bumps
// atomics.
type verdictCounter struct{ counts *metrics.Metrics }

func (v verdictCounter) Observe(d dns.Decision) { v.counts.CountVerdict(d.Action) }

// buildTLSConfig loads the operator's PEM pair for the encrypted listeners.
// Zero listeners and zero paths is the disabled default; anything between the
// two states is a configuration error, named here so a bad flag cannot hide
// behind first-boot seeding.
func buildTLSConfig(certPath, keyPath, dotAddress, dohAddress string) (*tls.Config, error) {
	switch {
	case certPath == "" && keyPath == "":
		if dotAddress != "" || dohAddress != "" {
			return nil, errors.New("dot-address or doh-address needs --tls-cert and --tls-key")
		}
		return nil, nil
	case certPath == "" || keyPath == "":
		return nil, errors.New("--tls-cert and --tls-key must be set together")
	case dotAddress == "" && dohAddress == "":
		return nil, errors.New("--tls-cert and --tls-key need --dot-address or --doh-address to serve on")
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("tls: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		// h2 is what makes DoH serve HTTP/2; DoT clients ignore the offer.
		NextProtos: []string{"h2", "http/1.1"},
	}, nil
}

func listenerAddrOrOff(addr net.Addr) string {
	if addr == nil {
		return "off"
	}
	return addr.String()
}

func newLogger(level string, out io.Writer) (*slog.Logger, error) {
	var parsed slog.Level
	if err := parsed.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("unknown log level %q", level)
	}
	return slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: parsed})), nil
}

func runThreatRefreshLoop(ctx context.Context, sync *runtime.ThreatSync, logger *slog.Logger) {
	ticker := time.NewTicker(sourceRefreshTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := sync.RefreshFeeds(ctx); err != nil {
				logger.Warn("threat feed refresh failed", "error", err)
			}
		}
	}
}

// seedThreatFeeds stores the flag feeds on first boot, the way blocklist
// sources seed: the database stays the source of truth after that.
func seedThreatFeeds(ctx context.Context, database *store.Store, entries []string, firstBoot bool, logger *slog.Logger) error {
	if !firstBoot || len(entries) == 0 {
		return nil
	}
	for _, raw := range entries {
		name, url, found := strings.Cut(raw, "=")
		if !found || name == "" || url == "" {
			return fmt.Errorf("threat-feed %q must be name=url", raw)
		}
		if err := database.SaveThreatFeed(ctx, store.ThreatFeed{Name: name, URL: url, Enabled: true}); err != nil {
			return err
		}
	}
	logger.Info("seeded threat feeds from the flags", "feeds", len(entries))
	return nil
}
