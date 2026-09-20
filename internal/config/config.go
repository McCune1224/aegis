// Package config loads the settings an aegis process runs from.
package config

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Config is the settings one aegis process runs from. Every field is text
// because these values arrive from flags and the environment, and the command
// parses each one into a typed value where it is used.
type Config struct {
	DNSAddress    string   `mapstructure:"dns-address"`
	Upstreams     []string `mapstructure:"upstream"`
	APIAddress    string   `mapstructure:"api-address"`
	DB            string   `mapstructure:"db"`
	BlockingMode  string   `mapstructure:"blocking-mode"`
	CustomAddress string   `mapstructure:"custom-address"`
	Blocklists    []string `mapstructure:"blocklist"`
	BlockFormat   string   `mapstructure:"block-format"`
	Profiles      []string `mapstructure:"profile"`
	Clients       []string `mapstructure:"client"`
	Sources       []string `mapstructure:"source"`
	Rewrites      []string `mapstructure:"rewrite"`
	SourceRefresh string   `mapstructure:"source-refresh"`
	RateLimit     float64  `mapstructure:"rate-limit"`
	RateBurst     int      `mapstructure:"rate-burst"`
	LogLevel      string   `mapstructure:"log-level"`
	DHCPAddress   string   `mapstructure:"dhcp-address"`
	DHCPRange     string   `mapstructure:"dhcp-range"`
	DHCPNetmask   string   `mapstructure:"dhcp-netmask"`
	DHCPServerIP  string   `mapstructure:"dhcp-server-ip"`
	DHCPRouter    string   `mapstructure:"dhcp-router"`
	DHCPDNS       []string `mapstructure:"dhcp-dns"`
	DHCPLeaseTime string   `mapstructure:"dhcp-lease-time"`
	DoTAddress    string   `mapstructure:"dot-address"`
	DoHAddress    string   `mapstructure:"doh-address"`
	TLSCert       string   `mapstructure:"tls-cert"`
	TLSKey        string   `mapstructure:"tls-key"`
}

// Load reads the environment over the flag defaults, and a flag that was set
// over both. The defaults live on the flags, so there is one place to change
// them.
//
// The environment prefix is AEGIS, so a container sets AEGIS_UPSTREAM rather
// than passing a flag.
func Load(flags *pflag.FlagSet) (Config, error) {
	v := viper.New()
	v.SetEnvPrefix("AEGIS")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()
	if err := v.BindPFlags(flags); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}
