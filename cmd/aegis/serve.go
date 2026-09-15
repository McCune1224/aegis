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
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"aegis/internal/blocklist"
	"aegis/internal/config"
	"aegis/internal/dns"
	"aegis/internal/filter"
)

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
	flags.String("blocking-mode", "nxdomain", "nxdomain, null-address, custom-address, or refused")
	flags.String("custom-address", "", "the address to answer with when blocking-mode is custom-address")
	flags.StringArray("blocklist", nil, "path to a blocklist file, repeatable")
	flags.String("block-format", "hosts", "hosts, domains, or adblock, applied to every blocklist")
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

	mode, err := dns.ParseBlockingMode(cfg.BlockingMode)
	if err != nil {
		return err
	}

	format, err := blocklist.ParseFormat(cfg.BlockFormat)
	if err != nil {
		return err
	}

	custom, err := parseOptionalAddress(cfg.CustomAddress)
	if err != nil {
		return err
	}

	rules, err := loadBlocklists(cfg.Blocklists, format, logger)
	if err != nil {
		return err
	}

	set, err := filter.Compile(rules)
	if err != nil {
		return err
	}
	engine := filter.New()
	engine.Publish(set)

	handler, err := dns.NewHandler(dns.Config{
		Engine:   engine,
		Upstream: dns.NewForwarder(cfg.Upstream),
		Mode:     mode,
		Custom:   custom,
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
		"blocking_mode", mode.String(),
	)

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	logger.Info("aegis is shutting down")
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(shutdown)
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

func newLogger(level string, out io.Writer) (*slog.Logger, error) {
	var parsed slog.Level
	if err := parsed.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("aegis: unknown log level %q", level)
	}
	return slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: parsed})), nil
}
