package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/require"

	"aegis/internal/blocklist"
	"aegis/internal/config"
	"aegis/internal/filter"
	"aegis/internal/store"
)

func freeAddress(t *testing.T) string {
	t.Helper()
	var listen net.ListenConfig
	packet, err := listen.ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)
	address := packet.LocalAddr().String()
	require.NoError(t, packet.Close())
	return address
}

func freeTCPAddress(t *testing.T) string {
	t.Helper()
	var listen net.ListenConfig
	ln, err := listen.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := ln.Addr().String()
	require.NoError(t, ln.Close())
	return address
}

func startUpstream(t *testing.T, answer string) string {
	t.Helper()
	var listen net.ListenConfig
	packet, err := listen.ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)

	server := &mdns.Server{
		PacketConn: packet,
		Handler: mdns.HandlerFunc(func(w mdns.ResponseWriter, req *mdns.Msg) {
			resp := new(mdns.Msg)
			resp.SetReply(req)
			resp.Answer = append(resp.Answer, &mdns.A{
				Hdr: mdns.RR_Header{Name: req.Question[0].Name, Rrtype: mdns.TypeA, Class: mdns.ClassINET, Ttl: 60},
				A:   netip.MustParseAddr(answer).AsSlice(),
			})
			_ = w.WriteMsg(resp)
		}),
	}
	go func() { _ = server.ActivateAndServe() }()
	t.Cleanup(func() { _ = server.Shutdown() })

	return packet.LocalAddr().String()
}

func ask(t *testing.T, address, name string) *mdns.Msg {
	t.Helper()
	client := &mdns.Client{Net: "udp", Timeout: 2 * time.Second}
	resp, _, err := client.Exchange(new(mdns.Msg).SetQuestion(name, mdns.TypeA), address)
	require.NoError(t, err)
	return resp
}

func TestServeCommandBlocksByRuleAndForwardsTheRest(t *testing.T) {
	upstream := startUpstream(t, "203.0.113.40")

	list := filepath.Join(t.TempDir(), "block.txt")
	require.NoError(t, os.WriteFile(list, []byte("0.0.0.0 ads.example.com\n"), 0o600))

	address := freeAddress(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := newRootCmd()
	cmd.SetArgs([]string{
		"serve",
		"--dns-address", address,
		"--upstream", upstream,
		"--db", filepath.Join(t.TempDir(), "aegis.db"),
		"--blocklist", list,
		"--log-level", "error",
	})
	cmd.SetContext(ctx)

	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()

	require.Eventually(t, func() bool {
		client := &mdns.Client{Net: "udp", Timeout: 200 * time.Millisecond}
		_, _, err := client.Exchange(new(mdns.Msg).SetQuestion("example.com.", mdns.TypeA), address)
		return err == nil
	}, 5*time.Second, 25*time.Millisecond, "serve never began answering")

	blocked := ask(t, address, "ads.example.com.")
	require.Equal(t, mdns.RcodeNameError, blocked.Rcode)
	require.Empty(t, blocked.Answer)

	allowed := ask(t, address, "example.com.")
	require.Equal(t, mdns.RcodeSuccess, allowed.Rcode)
	require.Len(t, allowed.Answer, 1)
	require.Equal(t, netip.MustParseAddr("203.0.113.40").AsSlice(), []byte(allowed.Answer[0].(*mdns.A).A))

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after its context was cancelled")
	}
}

func TestServeCommandServesTheAPI(t *testing.T) {
	upstream := startUpstream(t, "203.0.113.50")
	address := freeAddress(t)
	apiAddress := freeTCPAddress(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := newRootCmd()
	cmd.SetArgs([]string{
		"serve",
		"--dns-address", address,
		"--upstream", upstream,
		"--db", filepath.Join(t.TempDir(), "aegis.db"),
		"--api-address", apiAddress,
		"--log-level", "error",
	})
	cmd.SetContext(ctx)

	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()

	require.Eventually(t, func() bool {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+apiAddress+"/api/v1/profiles", nil)
		if err != nil {
			return false
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, 25*time.Millisecond, "serve never served the API")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after its context was cancelled")
	}
}

func TestServeCommandServesEncryptedDNSWithAGeneratedCertificate(t *testing.T) {
	upstream := startUpstream(t, "203.0.113.70")
	certPEM, keyPEM, pool := pinning(t)
	certDir := t.TempDir()
	certPath := filepath.Join(certDir, "cert.pem")
	keyPath := filepath.Join(certDir, "key.pem")
	require.NoError(t, os.WriteFile(certPath, certPEM, 0o600))
	require.NoError(t, os.WriteFile(keyPath, keyPEM, 0o600))

	list := filepath.Join(t.TempDir(), "block.txt")
	require.NoError(t, os.WriteFile(list, []byte("0.0.0.0 ads.example.com\n"), 0o600))

	address := freeAddress(t)
	dotAddress := freeTCPAddress(t)
	dohAddress := freeTCPAddress(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := newRootCmd()
	cmd.SetArgs([]string{
		"serve",
		"--dns-address", address,
		"--upstream", upstream,
		"--db", filepath.Join(t.TempDir(), "aegis.db"),
		"--blocklist", list,
		"--dot-address", dotAddress,
		"--doh-address", dohAddress,
		"--tls-cert", certPath,
		"--tls-key", keyPath,
		"--log-level", "error",
	})
	cmd.SetContext(ctx)

	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()

	tlsCfg := &tls.Config{RootCAs: pool, ServerName: "localhost", MinVersion: tls.VersionTLS12}
	require.Eventually(t, func() bool {
		_, _, err := (&mdns.Client{Net: "tcp-tls", TLSConfig: tlsCfg, Timeout: 500 * time.Millisecond}).
			Exchange(new(mdns.Msg).SetQuestion("example.com.", mdns.TypeA), dotAddress)
		return err == nil
	}, 5*time.Second, 50*time.Millisecond, "serve never began answering over DoT")

	// The plaintext, DoT, and DoH listeners agree on every answer, and the
	// blocked rule holds on the encrypted paths: the client reaches the
	// handler identified regardless of transport.
	plain := ask(t, address, "example.com.")
	require.Len(t, plain.Answer, 1)

	dot, _, err := (&mdns.Client{Net: "tcp-tls", TLSConfig: tlsCfg, Timeout: 2 * time.Second}).
		Exchange(new(mdns.Msg).SetQuestion("example.com.", mdns.TypeA), dotAddress)
	require.NoError(t, err)
	require.Equal(t, plain.Answer, dot.Answer, "DoT answers what plaintext answers")

	dotBlocked, _, err := (&mdns.Client{Net: "tcp-tls", TLSConfig: tlsCfg, Timeout: 2 * time.Second}).
		Exchange(new(mdns.Msg).SetQuestion("ads.example.com.", mdns.TypeA), dotAddress)
	require.NoError(t, err)
	require.Equal(t, mdns.RcodeNameError, dotBlocked.Rcode, "the blocklist holds on DoT")

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsCfg}}
	wire, err := new(mdns.Msg).SetQuestion("example.com.", mdns.TypeA).Pack()
	require.NoError(t, err)
	post := postDNS(t, client, "https://"+dohAddress+"/dns", wire)
	defer func() { _ = post.Body.Close() }()
	require.Equal(t, http.StatusOK, post.StatusCode)
	dohAnswer := new(mdns.Msg)
	require.NoError(t, dohAnswer.Unpack(readAll(t, post.Body)))
	require.Len(t, dohAnswer.Answer, 1)
	a, ok := dohAnswer.Answer[0].(*mdns.A)
	require.True(t, ok)
	require.Equal(t, netip.MustParseAddr("203.0.113.70").AsSlice(), []byte(a.A), "DoH answers what plaintext answers")

	blockedWire, err := new(mdns.Msg).SetQuestion("ads.example.com.", mdns.TypeA).Pack()
	require.NoError(t, err)
	blockedPost := postDNS(t, client, "https://"+dohAddress+"/dns", blockedWire)
	defer func() { _ = blockedPost.Body.Close() }()
	blockedAnswer := new(mdns.Msg)
	require.NoError(t, blockedAnswer.Unpack(readAll(t, blockedPost.Body)))
	require.Equal(t, mdns.RcodeNameError, blockedAnswer.Rcode, "the blocklist holds on DoH")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after its context was cancelled")
	}
}

// postDNS sends one wire-format DoH query with the request bound to the test
// context, the form the noctx lint demands.
func postDNS(t *testing.T, client *http.Client, url string, wire []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, bytes.NewReader(wire))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/dns-message")
	resp, err := client.Do(req)
	require.NoError(t, err)
	return resp
}

func modePtr(mode filter.BlockingMode) *filter.BlockingMode { return &mode }

func TestASecondBootLeavesTheStoredConfigurationAlone(t *testing.T) {
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "aegis.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	first, err := database.FirstBoot(ctx)
	require.NoError(t, err)
	require.True(t, first)

	// The operator changes the default profile in the UI, leaving one profile and
	// no clients, which used to look like an empty database again.
	require.NoError(t, database.SaveProfile(ctx, filter.ProfileSpec{ID: "default", Mode: modePtr(filter.Refused)}))
	require.NoError(t, database.MarkSeeded(ctx))

	first, err = database.FirstBoot(ctx)
	require.NoError(t, err)
	require.False(t, first)
	require.NoError(t, seed(ctx, database, []filter.ProfileSpec{{ID: "default", Mode: modePtr(filter.NullAddress)}}, nil, first, logger))

	cfg, err := database.Load(ctx)
	require.NoError(t, err)
	require.Len(t, cfg.Profiles, 1)
	require.NotNil(t, cfg.Profiles[0].Mode)
	require.Equal(t, filter.Refused, *cfg.Profiles[0].Mode)
}

func TestServeCommandLoadsRulesFromASourceAndSurvivesABadOne(t *testing.T) {
	upstream := startUpstream(t, "203.0.113.60")
	list := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "0.0.0.0 ads.example.com\n")
	}))
	t.Cleanup(list.Close)

	address := freeAddress(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := newRootCmd()
	cmd.SetArgs([]string{
		"serve",
		"--dns-address", address,
		"--api-address", freeTCPAddress(t),
		"--upstream", upstream,
		"--db", filepath.Join(t.TempDir(), "aegis.db"),
		"--source", "good=" + list.URL,
		"--source", "broken=http://127.0.0.1:1/list",
		"--log-level", "error",
	})
	cmd.SetContext(ctx)

	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()

	require.Eventually(t, func() bool {
		client := &mdns.Client{Net: "udp", Timeout: 200 * time.Millisecond}
		_, _, err := client.Exchange(new(mdns.Msg).SetQuestion("example.com.", mdns.TypeA), address)
		return err == nil
	}, 5*time.Second, 25*time.Millisecond, "serve never began answering")

	blocked := ask(t, address, "ads.example.com.")
	require.Equal(t, mdns.RcodeNameError, blocked.Rcode)
	require.Empty(t, blocked.Answer)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after its context was cancelled")
	}
}

func TestServeCommandSeedsRewritesAndAnswersThemLocally(t *testing.T) {
	upstream := startUpstream(t, "203.0.113.40")

	address := freeAddress(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := newRootCmd()
	cmd.SetArgs([]string{
		"serve",
		"--dns-address", address,
		"--upstream", upstream,
		"--db", filepath.Join(t.TempDir(), "aegis.db"),
		"--rewrite", "nas.local=192.0.2.44",
		"--log-level", "error",
	})
	cmd.SetContext(ctx)

	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()

	require.Eventually(t, func() bool {
		client := &mdns.Client{Net: "udp", Timeout: 200 * time.Millisecond}
		_, _, err := client.Exchange(new(mdns.Msg).SetQuestion("example.com.", mdns.TypeA), address)
		return err == nil
	}, 5*time.Second, 25*time.Millisecond, "serve never began answering")

	rewritten := ask(t, address, "nas.local.")
	require.Equal(t, mdns.RcodeSuccess, rewritten.Rcode)
	require.Len(t, rewritten.Answer, 1)
	require.Equal(t, netip.MustParseAddr("192.0.2.44").AsSlice(), []byte(rewritten.Answer[0].(*mdns.A).A))

	// A second boot keeps the seeded rewrite without the flag re-adding it.
	ptr := askPTR(t, address, "44.2.0.192.in-addr.arpa.")
	require.Equal(t, mdns.RcodeSuccess, ptr.Rcode)
	require.Len(t, ptr.Answer, 1)
	require.Equal(t, "nas.local.", ptr.Answer[0].(*mdns.PTR).Ptr)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after its context was cancelled")
	}
}

func askPTR(t *testing.T, address, name string) *mdns.Msg {
	t.Helper()
	client := &mdns.Client{Net: "udp", Timeout: 2 * time.Second}
	resp, _, err := client.Exchange(new(mdns.Msg).SetQuestion(name, mdns.TypePTR), address)
	require.NoError(t, err)
	return resp
}

func TestServeCommandLoadsAHostsListAndAnAdblockListTogether(t *testing.T) {
	upstream := startUpstream(t, "203.0.113.80")

	hosts := filepath.Join(t.TempDir(), "hosts.txt")
	require.NoError(t, os.WriteFile(hosts, []byte("0.0.0.0 ads.example.com\n"), 0o600))
	adblock := filepath.Join(t.TempDir(), "adblock.txt")
	require.NoError(t, os.WriteFile(adblock, []byte("||trackers.example.com^\n"), 0o600))

	servedHosts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "0.0.0.0 served-hosts.example.com\n")
	}))
	t.Cleanup(servedHosts.Close)
	servedAdblock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "||served-adblock.example.com^\n")
	}))
	t.Cleanup(servedAdblock.Close)

	address := freeAddress(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := newRootCmd()
	cmd.SetArgs([]string{
		"serve",
		"--dns-address", address,
		"--api-address", freeTCPAddress(t),
		"--upstream", upstream,
		"--db", filepath.Join(t.TempDir(), "aegis.db"),
		"--blocklist", hosts,
		"--blocklist", "adblock:" + adblock,
		"--source", "sh=" + servedHosts.URL,
		"--source", "sa=adblock:" + servedAdblock.URL,
		"--log-level", "error",
	})
	cmd.SetContext(ctx)

	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()

	require.Eventually(t, func() bool {
		client := &mdns.Client{Net: "udp", Timeout: 200 * time.Millisecond}
		_, _, err := client.Exchange(new(mdns.Msg).SetQuestion("example.com.", mdns.TypeA), address)
		return err == nil
	}, 5*time.Second, 25*time.Millisecond, "serve never began answering")

	// Every list is parsed in its own format: the hosts file blocks as a
	// hosts entry, the adblock file blocks as a rule, and the fetched
	// sources do the same in one run.
	for _, name := range []string{
		"ads.example.com.",
		"trackers.example.com.",
		"served-hosts.example.com.",
		"served-adblock.example.com.",
	} {
		blocked := ask(t, address, name)
		require.Equal(t, mdns.RcodeNameError, blocked.Rcode, "case=%s must block", name)
		require.Empty(t, blocked.Answer)
	}

	allowed := ask(t, address, "example.com.")
	require.Equal(t, mdns.RcodeSuccess, allowed.Rcode, "the formats must not swallow the allow path")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after its context was cancelled")
	}
}

func TestParseBlocklistEntryAndSourceTakeAnExactFormatPrefix(t *testing.T) {
	hosts := mustFormat(t, "hosts")

	list := parseBlocklistEntry("adblock:/etc/lists/x.txt", hosts)
	require.Equal(t, "/etc/lists/x.txt", list.Path)
	require.Equal(t, mustFormat(t, "adblock"), list.Format)

	list = parseBlocklistEntry("/etc/lists/plain.txt", hosts)
	require.Equal(t, "/etc/lists/plain.txt", list.Path)
	require.Equal(t, hosts, list.Format, "a bare path keeps the fallback format")

	// A drive-letter path or any other colon-bearing string that does not
	// name a format is a path, not an error.
	list = parseBlocklistEntry("C:\\lists\\x.txt", hosts)
	require.Equal(t, "C:\\lists\\x.txt", list.Path)
	require.Equal(t, hosts, list.Format)

	source, err := parseSource("mix=adblock:https://lists.example.com/a?dl=1", hosts)
	require.NoError(t, err)
	require.Equal(t, mustFormat(t, "adblock"), source.Format)
	require.Equal(t, "https://lists.example.com/a?dl=1", source.URL, "the url keeps its colons and equals")

	source, err = parseSource("plain=https://lists.example.com/a?dl=1", hosts)
	require.NoError(t, err)
	require.Equal(t, hosts, source.Format, "a url without a prefix takes the fallback format")
	require.Equal(t, "https://lists.example.com/a?dl=1", source.URL)
}

func mustFormat(t *testing.T, name string) blocklist.Format {
	t.Helper()
	format, err := blocklist.ParseFormat(name)
	require.NoError(t, err)
	return format
}

func TestServeCommandReportsWhatItCannotParse(t *testing.T) {
	certDir := t.TempDir()
	certPEM, keyPEM, _ := pinning(t)
	certPath := filepath.Join(certDir, "cert.pem")
	keyPath := filepath.Join(certDir, "key.pem")
	require.NoError(t, os.WriteFile(certPath, certPEM, 0o600))
	require.NoError(t, os.WriteFile(keyPath, keyPEM, 0o600))

	cases := map[string][]string{
		"unknown blocking mode":     {"--blocking-mode", "drop"},
		"unknown block format":      {"--block-format", "csv"},
		"unknown log level":         {"--log-level", "chatty"},
		"bad custom address":        {"--custom-address", "not-an-address"},
		"missing blocklist":         {"--blocklist", "/nonexistent/list.txt"},
		"bad upstream scheme":       {"--upstream", "ftp://9.9.9.9"},
		"bad rewrite":               {"--rewrite", "nas.local=not a target"},
		"dot without a certificate": {"--dot-address", "127.0.0.1:0"},
		"doh without a certificate": {"--doh-address", "127.0.0.1:0"},
		"half a certificate pair":   {"--tls-cert", certPath},
		"certificates with no encrypted listener": {
			"--tls-cert", certPath, "--tls-key", keyPath,
		},
		"a certificate that is no certificate": {
			"--tls-cert", filepath.Join(certDir, "nope.pem"), "--tls-key", keyPath,
			"--dot-address", "127.0.0.1:0",
		},
	}
	for name, extra := range cases {
		args := append([]string{"serve", "--dns-address", "127.0.0.1:0"}, extra...)

		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		cmd := newRootCmd()
		cmd.SetArgs(args)
		cmd.SetContext(ctx)

		done := make(chan error, 1)
		go func() { done <- cmd.Execute() }()

		select {
		case err := <-done:
			require.Error(t, err, "case=%q", name)
		case <-time.After(2 * time.Second):
			cancel()
			t.Fatalf("case %q began serving instead of reporting an error", name)
		}
	}
}

// A mapstructure tag that names no flag reads as working config and silently
// yields the zero value. The blocklist flag hid behind exactly that, so this
// checks the tags against the flags instead of trusting them.
func TestServeCommandRefusesABurstOverTheClientRateLimit(t *testing.T) {
	upstream := startUpstream(t, "203.0.113.40")

	address := freeAddress(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := newRootCmd()
	cmd.SetArgs([]string{
		"serve",
		"--dns-address", address,
		"--upstream", upstream,
		"--db", filepath.Join(t.TempDir(), "aegis.db"),
		"--log-level", "error",
		"--rate-limit", "1",
		"--rate-burst", "2",
	})
	cmd.SetContext(ctx)

	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()

	require.Eventually(t, func() bool {
		client := &mdns.Client{Net: "udp", Timeout: 200 * time.Millisecond}
		_, _, err := client.Exchange(new(mdns.Msg).SetQuestion("example.com.", mdns.TypeA), address)
		return err == nil
	}, 5*time.Second, 25*time.Millisecond, "serve never began answering")

	time.Sleep(2500 * time.Millisecond)

	var allowed, refused int
	for i := 0; i < 5; i++ {
		got := ask(t, address, "example.com.")
		switch got.Rcode {
		case mdns.RcodeSuccess:
			allowed++
		case mdns.RcodeRefused:
			refused++
		}
	}
	require.Equal(t, 2, allowed, "the burst of two goes through")
	require.Equal(t, 3, refused, "every query past the burst is refused")

	time.Sleep(1200 * time.Millisecond)
	require.Equal(t, mdns.RcodeSuccess, ask(t, address, "example.com.").Rcode, "a query at one per second sits under the limit")
	cancel()
	require.NoError(t, <-done)
}

func TestEveryConfigFieldNamesAServeFlag(t *testing.T) {
	flags := newServeCmd().Flags()

	for _, field := range reflect.VisibleFields(reflect.TypeFor[config.Config]()) {
		tag := field.Tag.Get("mapstructure")
		if tag == "" || tag == "-" {
			t.Errorf("config field %s has no mapstructure tag", field.Name)
			continue
		}
		if flags.Lookup(tag) == nil {
			t.Errorf("config field %s names %q, which is not a serve flag", field.Name, tag)
		}
	}
}

func TestServeCommandExposesMetricsThatMoveWithQueries(t *testing.T) {
	upstream := startUpstream(t, "203.0.113.90")
	list := filepath.Join(t.TempDir(), "block.txt")
	require.NoError(t, os.WriteFile(list, []byte("0.0.0.0 ads.example.com\n"), 0o600))

	address := freeAddress(t)
	apiAddress := freeTCPAddress(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := newRootCmd()
	cmd.SetArgs([]string{
		"serve",
		"--dns-address", address,
		"--upstream", upstream,
		"--db", filepath.Join(t.TempDir(), "aegis.db"),
		"--blocklist", list,
		"--api-address", apiAddress,
		"--log-level", "error",
	})
	cmd.SetContext(ctx)

	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()

	// The readiness probe asks warmup.example.com, a name no later
	// assertion counts, so every metric below is exact: one blocked ask,
	// two allowed asks, of which the repeat is answered from the cache.
	require.Eventually(t, func() bool {
		client := &mdns.Client{Net: "udp", Timeout: 200 * time.Millisecond}
		_, _, err := client.Exchange(new(mdns.Msg).SetQuestion("warmup.example.com.", mdns.TypeA), address)
		return err == nil
	}, 5*time.Second, 25*time.Millisecond, "serve never began answering")

	// One blocked query, one allowed, and a repeat of the allowed one so the
	// cache counters move too.
	blocked := ask(t, address, "ads.example.com.")
	require.Equal(t, mdns.RcodeNameError, blocked.Rcode)
	allowed := ask(t, address, "example.com.")
	require.Equal(t, mdns.RcodeSuccess, allowed.Rcode)
	repeat := ask(t, address, "example.com.")
	require.Equal(t, mdns.RcodeSuccess, repeat.Rcode)

	body := getBody(t, "http://"+apiAddress+"/metrics")
	require.Contains(t, body, `aegis_queries_total{verdict="blocked"} 1`, "the blocked query must move the verdict counter")
	require.Contains(t, body, `aegis_queries_total{verdict="allowed"} 3`, "the warmup ask plus the pair")
	require.Contains(t, body, `aegis_cache_hits_total 1`, "the repeated ask came from the cache")
	require.Contains(t, body, `aegis_cache_misses_total 2`)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after its context was cancelled")
	}
}

func getBody(t *testing.T, url string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	payload, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(payload)
}
