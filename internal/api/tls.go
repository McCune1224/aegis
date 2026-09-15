package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"strings"
	"time"
)

// serverTLS returns the TLS config a listener should use, or nil for plain
// HTTP. A supplied certificate wins over a self-signed one.
func serverTLS(cfg Config) (*tls.Config, error) {
	if cfg.CertFile == "" && cfg.KeyFile == "" && !cfg.SelfSigned {
		return nil, nil
	}
	if (cfg.CertFile == "") != (cfg.KeyFile == "") {
		return nil, errors.New("api: CertFile and KeyFile must be set together")
	}
	if cfg.CertFile != "" {
		certificate, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("api: load certificate: %w", err)
		}
		return &tls.Config{
			Certificates: []tls.Certificate{certificate},
			MinVersion:   tls.VersionTLS12,
		}, nil
	}
	return selfSignedTLS(cfg.Address)
}

func selfSignedTLS(address string) (*tls.Config, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("api: address %q: %w", address, err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("api: generate key: %w", err)
	}

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "aegis"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	switch {
	case host == "":
	case net.ParseIP(host) != nil:
		template.IPAddresses = append(template.IPAddresses, net.ParseIP(host))
	default:
		template.DNSNames = append(template.DNSNames, host)
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("api: create certificate: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// loopbackAddress reports whether the listen address is loopback only. An
// address a caller cannot confirm as loopback is treated as remote.
func loopbackAddress(address string) (bool, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false, fmt.Errorf("api: address %q: %w", address, err)
	}
	if host == "" {
		return false, nil
	}
	if strings.EqualFold(host, "localhost") {
		return true, nil
	}
	if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
		return ip.IsLoopback(), nil
	}
	return false, nil
}
