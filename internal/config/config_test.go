package config_test

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	"aegis/internal/config"
)

func serveFlags() *pflag.FlagSet {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("dns-address", "127.0.0.1:53", "")
	flags.String("upstream", "9.9.9.9:53", "")
	flags.String("db", "aegis.db", "")
	flags.String("blocking-mode", "nxdomain", "")
	flags.String("custom-address", "", "")
	flags.StringArray("blocklist", nil, "")
	flags.String("block-format", "hosts", "")
	flags.StringArray("profile", nil, "")
	flags.StringArray("client", nil, "")
	flags.String("source-refresh", "6h", "")
	flags.Float64("rate-limit", 0, "")
	flags.Int("rate-burst", 0, "")
	flags.String("log-level", "info", "")
	return flags
}

func TestLoadReadsEveryFlagValue(t *testing.T) {
	flags := serveFlags()
	require.NoError(t, flags.Parse([]string{
		"--dns-address", "0.0.0.0:5353",
		"--upstream", "1.1.1.1:53",
		"--db", "/tmp/aegis.db",
		"--blocking-mode", "refused",
		"--custom-address", "192.0.2.1",
		"--blocklist", "/lists/one.txt",
		"--blocklist", "/lists/two.txt",
		"--block-format", "adblock",
		"--profile", "kids=nxdomain",
		"--client", "10.0.0.5=kids",
		"--source-refresh", "12h",
		"--rate-limit", "20",
		"--rate-burst", "50",
		"--log-level", "debug",
	}))

	got, err := config.Load(flags)

	require.NoError(t, err)
	require.Equal(t, config.Config{
		DNSAddress:    "0.0.0.0:5353",
		Upstream:      "1.1.1.1:53",
		DB:            "/tmp/aegis.db",
		BlockingMode:  "refused",
		CustomAddress: "192.0.2.1",
		Blocklists:    []string{"/lists/one.txt", "/lists/two.txt"},
		BlockFormat:   "adblock",
		Profiles:      []string{"kids=nxdomain"},
		Clients:       []string{"10.0.0.5=kids"},
		SourceRefresh: "12h",
		RateLimit:     20,
		RateBurst:     50,
		LogLevel:      "debug",
	}, got)
}

func TestLoadFallsBackToTheFlagDefaults(t *testing.T) {
	flags := serveFlags()
	require.NoError(t, flags.Parse(nil))

	got, err := config.Load(flags)

	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:53", got.DNSAddress)
	require.Equal(t, "9.9.9.9:53", got.Upstream)
	require.Equal(t, "nxdomain", got.BlockingMode)
	require.Equal(t, "hosts", got.BlockFormat)
	require.Equal(t, "info", got.LogLevel)
	require.Empty(t, got.Blocklists)
}

func TestLoadPrefersTheEnvironmentOverTheFlagDefault(t *testing.T) {
	t.Setenv("AEGIS_UPSTREAM", "8.8.8.8:53")
	flags := serveFlags()
	require.NoError(t, flags.Parse(nil))

	got, err := config.Load(flags)

	require.NoError(t, err)
	require.Equal(t, "8.8.8.8:53", got.Upstream)
}

func TestLoadPrefersASetFlagOverTheEnvironment(t *testing.T) {
	t.Setenv("AEGIS_UPSTREAM", "8.8.8.8:53")
	flags := serveFlags()
	require.NoError(t, flags.Parse([]string{"--upstream", "1.1.1.1:53"}))

	got, err := config.Load(flags)

	require.NoError(t, err)
	require.Equal(t, "1.1.1.1:53", got.Upstream)
}
