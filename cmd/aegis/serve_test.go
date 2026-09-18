package main

import (
	"context"
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

func TestServeCommandReportsWhatItCannotParse(t *testing.T) {
	cases := map[string][]string{
		"unknown blocking mode": {"--blocking-mode", "drop"},
		"unknown block format":  {"--block-format", "csv"},
		"unknown log level":     {"--log-level", "chatty"},
		"bad custom address":    {"--custom-address", "not-an-address"},
		"missing blocklist":     {"--blocklist", "/nonexistent/list.txt"},
		"bad upstream scheme":   {"--upstream", "ftp://9.9.9.9"},
		"bad rewrite":           {"--rewrite", "nas.local=not a target"},
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
