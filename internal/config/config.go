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
	DNSAddress     string   `mapstructure:"dns-address"`
	Upstream       string   `mapstructure:"upstream"`
	APIAddress     string   `mapstructure:"api-address"`
	APIAllowRemote bool     `mapstructure:"api-allow-remote"`
	AdminPassword  string   `mapstructure:"admin-password"`
	TLSCert        string   `mapstructure:"tls-cert"`
	TLSKey         string   `mapstructure:"tls-key"`
	TLSSelfSigned  bool     `mapstructure:"tls-self-signed"`
	DB             string   `mapstructure:"db"`
	BlockingMode   string   `mapstructure:"blocking-mode"`
	CustomAddress  string   `mapstructure:"custom-address"`
	Blocklists     []string `mapstructure:"blocklist"`
	BlockFormat    string   `mapstructure:"block-format"`
	Profiles       []string `mapstructure:"profile"`
	Clients        []string `mapstructure:"client"`
	LogLevel       string   `mapstructure:"log-level"`
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
