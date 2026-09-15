package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"aegis/internal/blocklist"
	"aegis/internal/client"
	"aegis/internal/config"
	"aegis/internal/dns"
	"aegis/internal/filter"
)

// defaultProfile is the profile every client gets until a client entry names
// another one.
const defaultProfile filter.ProfileID = "default"

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
	flags.String("blocking-mode", "nxdomain", "nxdomain, null-address, custom-address, or refused, for the default profile")
	flags.String("custom-address", "", "the address to answer with when the default profile blocks")
	flags.StringArray("blocklist", nil, "path to a blocklist file, repeatable")
	flags.String("block-format", "hosts", "hosts, domains, or adblock, applied to every blocklist")
	flags.StringArray("profile", nil, "extra profile as name=mode or name=mode=address, repeatable")
	flags.StringArray("client", nil, "bind an address to a profile as address=profile, repeatable")
	flags.String("log-level", "info", "debug, info, warn, or error")

	return cmd
}

func runServe(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load(cmd.Flags())
	if err != nil {
		return err
	}

	logger, err := newLogger(cfg.LogLevel, cmd.ErrOrStderr())
	if err != nil {
		return err
	}

	format, err := blocklist.ParseFormat(cfg.BlockFormat)
	if err != nil {
		return err
	}

	rules, err := loadBlocklists(cfg.Blocklists, format, logger)
	if err != nil {
		return err
	}

	profiles, err := buildProfiles(cfg)
	if err != nil {
		return err
	}

	clientSpecs, addresses, err := buildClients(cfg.Clients)
	if err != nil {
		return err
	}

	identity, err := client.New(addresses)
	if err != nil {
		return err
	}

	set, err := filter.Compile(filter.Config{
		Rules:    rules,
		Profiles: profiles,
		Clients:  clientSpecs,
		Default:  defaultProfile,
	})
	if err != nil {
		return err
	}
	engine := filter.New()
	engine.Publish(set)

	handler, err := dns.NewHandler(dns.Config{
		Engine:   engine,
		Upstream: dns.NewForwarder(cfg.Upstream),
		Clients:  identity,
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

	logger.Info("aegis is serving",
		"udp", server.UDPAddr().String(),
		"tcp", server.TCPAddr().String(),
		"upstream", cfg.Upstream,
		"rules", len(rules),
		"profiles", len(profiles),
		"clients", len(clientSpecs),
	)

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	logger.Info("aegis is shutting down")
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(shutdown)
}

// buildProfiles turns the default settings and any configured profiles into the
// specs Compile resolves.
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
		return filter.ProfileSpec{}, fmt.Errorf("aegis: profile %q must be name=mode or name=mode=address", raw)
	}

	mode, err := filter.ParseBlockingMode(parts[1])
	if err != nil {
		return filter.ProfileSpec{}, err
	}
	profile := filter.ProfileSpec{ID: filter.ProfileID(parts[0]), Mode: &mode}

	if len(parts) == 3 {
		address, err := netip.ParseAddr(parts[2])
		if err != nil {
			return filter.ProfileSpec{}, fmt.Errorf("aegis: profile %q: %w", raw, err)
		}
		profile.Custom = &address
	}
	return profile, nil
}

// buildClients returns the policy bindings and the identity bindings for the
// same set of entries, so the two can never disagree. The address doubles as
// the client key, which makes a verdict readable without a lookup table.
func buildClients(entries []string) ([]filter.ClientSpec, []client.Spec, error) {
	var policies []filter.ClientSpec
	var addresses []client.Spec

	for _, raw := range entries {
		addressText, profile, found := strings.Cut(raw, "=")
		if !found || profile == "" {
			return nil, nil, fmt.Errorf("aegis: client %q must be address=profile", raw)
		}
		address, err := netip.ParseAddr(addressText)
		if err != nil {
			return nil, nil, fmt.Errorf("aegis: client %q: %w", raw, err)
		}

		key := filter.ClientKey(address.String())
		policies = append(policies, filter.ClientSpec{Key: key, Profile: filter.ProfileID(profile)})
		addresses = append(addresses, client.Spec{Key: key, Addresses: []netip.Addr{address}})
	}
	return policies, addresses, nil
}

// loadBlocklists reads every configured file in one format and returns the
// rules together. A file that cannot be read fails the start rather than
// silently serving fewer rules than the operator asked for.
func loadBlocklists(paths []string, format blocklist.Format, logger *slog.Logger) ([]filter.RuleSpec, error) {
	var rules []filter.RuleSpec
	for _, path := range paths {
		result, err := readList(path, format)
		if err != nil {
			return nil, err
		}

		logger.Info("blocklist loaded", "path", path, "rules", len(result.Rules), "skipped", result.Skipped)
		rules = append(rules, result.Rules...)
	}
	return rules, nil
}

func readList(path string, format blocklist.Format) (blocklist.ParseResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return blocklist.ParseResult{}, fmt.Errorf("aegis: %w", err)
	}

	source := filter.Source{ID: path, Name: filepath.Base(path)}
	result, parseErr := blocklist.ParseList(file, source, format)
	closeErr := file.Close()

	if parseErr != nil {
		return blocklist.ParseResult{}, parseErr
	}
	if closeErr != nil {
		return blocklist.ParseResult{}, fmt.Errorf("aegis: %w", closeErr)
	}
	return result, nil
}

func parseOptionalAddress(raw string) (netip.Addr, error) {
	if raw == "" {
		return netip.Addr{}, nil
	}
	address, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("aegis: custom-address: %w", err)
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
		return nil, fmt.Errorf("aegis: unknown log level %q", level)
	}
	return slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: parsed})), nil
}
