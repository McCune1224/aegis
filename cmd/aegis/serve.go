package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	"aegis/internal/config"
	"aegis/internal/dns"
	"aegis/internal/filter"
	"aegis/internal/querylog"
	"aegis/internal/runtime"
	"aegis/internal/store"
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

func newServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the DNS server",
		Args:  cobra.NoArgs,
		RunE:  runServe,
	}

	flags := cmd.Flags()
	flags.String("dns-address", "127.0.0.1:53", "address to listen on, as host:port")
	flags.String("upstream", "9.9.9.9:53", "upstream resolver, as host:port")
	flags.String("api-address", "127.0.0.1:8080", "address for the HTTP API, as host:port")
	flags.String("db", "aegis.db", "path to the configuration database")
	flags.String("blocking-mode", "nxdomain", "nxdomain, null-address, custom-address, or refused, for the default profile")
	flags.String("custom-address", "", "the address to answer with when the default profile blocks")
	flags.StringArray("blocklist", nil, "path to a blocklist file, repeatable")
	flags.StringArray("source", nil, "blocklist source as name=url, repeatable")
	flags.String("block-format", "hosts", "hosts, domains, or adblock, applied to every blocklist")
	flags.StringArray("profile", nil, "extra profile as name=mode or name=mode=address, repeatable")
	flags.StringArray("client", nil, "bind an address to a profile as address=profile, repeatable")
	flags.String("source-refresh", "6h", "how often to fetch blocklist sources for updates, as a duration")
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

	lists := make([]runtime.ListFile, 0, len(cfg.Blocklists))
	for _, path := range cfg.Blocklists {
		lists = append(lists, runtime.ListFile{Path: path, Format: format})
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
	if err := database.MarkSeeded(ctx); err != nil {
		return err
	}
	engine := runtime.New(database, lists, logger)
	sync := runtime.NewSourceSync(database, blocklist.NewFetcher(sourceTimeout), engine, logger)
	if err := sync.RefreshSources(ctx); err != nil {
		return err
	}

	hub := api.NewHub(logger)
	log := querylog.New(database, logger)

	resolver, err := cache.New(cache.Config{Upstream: dns.NewForwarder(cfg.Upstream)})
	if err != nil {
		return err
	}
	handler, err := dns.NewHandler(dns.Config{
		Decider:   engine,
		Upstream:  resolver,
		Observers: []dns.Observer{hub, log},
	})
	if err != nil {
		return err
	}

	server, err := dns.Start(dns.ServerConfig{
		Handler: handler,
		Address: cfg.DNSAddress,
		Logger:  logger,
	})
	if err != nil {
		return err
	}

	apiServer, err := api.Start(api.Config{
		Store:    database,
		Reloader: engine,
		Sources:  sync,
		Preview:  sync,
		Hub:      hub,
		Files:    web.Files(),
		Upstream: cfg.Upstream,
		Address:  cfg.APIAddress,
		Logger:   logger,
	})
	if err != nil {
		_ = server.Shutdown(context.Background())
		return err
	}

	logger.Info("aegis is serving",
		"udp", server.UDPAddr().String(),
		"tcp", server.TCPAddr().String(),
		"api", apiServer.Addr().String(),
		"upstream", cfg.Upstream,
		"database", cfg.DB,
		"rules", engine.Size(),
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

	<-stop.Done()

	logger.Info("aegis is shutting down")
	if err := log.Close(); err != nil {
		logger.Warn("querylog: final flush failed", "error", err)
	}
	shutdown, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	return errors.Join(apiServer.Shutdown(shutdown), server.Shutdown(shutdown))
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

func parseSource(raw string, format blocklist.Format) (store.Source, error) {
	name, url, found := strings.Cut(raw, "=")
	if !found || name == "" || url == "" {
		return store.Source{}, fmt.Errorf("source %q must be name=url", raw)
	}
	return store.Source{Name: name, URL: url, Format: format, Enabled: true}, nil
}

// buildProfiles turns the default settings and any configured profiles into the
// specs the store holds.
func buildProfiles(cfg config.Config) ([]filter.ProfileSpec, error) {
	mode, err := filter.ParseBlockingMode(cfg.BlockingMode)
	if err != nil {
		return nil, err
	}
	custom, err := parseOptionalAddress(cfg.CustomAddress)
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

func parseOptionalAddress(raw string) (netip.Addr, error) {
	if raw == "" {
		return netip.Addr{}, nil
	}
	address, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("custom-address: %w", err)
	}
	return address, nil
}

func customPointer(address netip.Addr) *netip.Addr {
	if !address.IsValid() {
		return nil
	}
	return &address
}

func newLogger(level string, out io.Writer) (*slog.Logger, error) {
	var parsed slog.Level
	if err := parsed.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("unknown log level %q", level)
	}
	return slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: parsed})), nil
}
