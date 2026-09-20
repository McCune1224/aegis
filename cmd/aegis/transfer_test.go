package main

import (
	"io"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/require"

	"aegis/internal/blocklist"
	"aegis/internal/filter"
	"aegis/internal/rewrite"
	"aegis/internal/runtime"
	"aegis/internal/store"
)

func runAegis(t *testing.T, args ...string) {
	t.Helper()
	root := newRootCmd()
	root.SetArgs(args)
	require.NoError(t, root.Execute())
}

func openStoreForTest(t *testing.T, path string) *store.Store {
	t.Helper()
	database, err := store.Open(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func parseName(t *testing.T, name string) filter.Domain {
	t.Helper()
	domain, err := filter.ParseDomain(name)
	require.NoError(t, err)
	return domain
}

func parseTarget(t *testing.T, pattern, target string) rewrite.Record {
	t.Helper()
	record, err := rewrite.Parse(pattern, target)
	require.NoError(t, err)
	return record
}

func answerOf(t *testing.T, resp *mdns.Msg) string {
	t.Helper()
	a, ok := resp.Answer[0].(*mdns.A)
	require.True(t, ok, "expected an A answer, got %T", resp.Answer[0])
	return netip.AddrFrom4([4]byte(a.A)).String()
}

// populateTransferStore fills one store with every section the document
// carries, the way serve's seeders would. The upstream rows point at live
// stubs and the source row at a local list file, so the imported store can be
// driven against the same resolvers.
func populateTransferStore(t *testing.T, path, primary, spare, list string) {
	t.Helper()
	ctx := t.Context()
	database := openStoreForTest(t, path)

	require.NoError(t, database.SaveProfile(ctx, filter.ProfileSpec{ID: "kids", Mode: modePtr(filter.Refused)}))
	require.NoError(t, database.SaveClient(ctx, store.Client{
		Key:       "tablet",
		Profile:   "kids",
		Notes:     "the spare one",
		Addresses: []netip.Addr{netip.MustParseAddr("10.9.9.2")},
	}))
	require.NoError(t, database.SaveSchedule(ctx, filter.ScheduleSpec{
		Name:     "night",
		Priority: 2,
		Windows:  []filter.Window{{Days: []time.Weekday{time.Monday}, Start: 21 * 60, End: 7 * 60}},
	}))
	_, err := database.SaveRule(ctx, store.Rule{
		Kind:   filter.MatchExact,
		Domain: parseName(t, "games.example"),
		Action: filter.ActionBlock,
	})
	require.NoError(t, err)
	require.NoError(t, database.SaveRewrite(ctx, parseTarget(t, "nas.example", "10.9.9.77")))
	require.NoError(t, database.SaveUpstream(ctx, store.Upstream{Name: "primary", URL: primary, Enabled: true}))
	require.NoError(t, database.SaveUpstream(ctx, store.Upstream{Name: "spare", URL: spare, Enabled: true, Backup: true}))
	_, err = database.SaveRoute(ctx, store.Route{Domain: "routed.example", Upstream: "primary"})
	require.NoError(t, err)
	require.NoError(t, database.SetAccess(ctx, []netip.Prefix{netip.MustParsePrefix("10.9.8.0/24")}, nil))
	require.NoError(t, database.SaveSource(ctx, store.Source{
		Name:    "local",
		URL:     "file://" + list,
		Format:  blocklist.FormatHosts,
		Enabled: true,
	}))
}

func exportFixture(t *testing.T) (document, list string) {
	t.Helper()
	dir := t.TempDir()
	list = filepath.Join(dir, "list.txt")
	require.NoError(t, os.WriteFile(list, []byte("0.0.0.0 listed.example.com\n"), 0o644))
	return filepath.Join(dir, "config.json"), list
}

func TestAnExportImportsIntoAnEmptyStoreAndProducesTheSameVerdicts(t *testing.T) {
	ctx := t.Context()
	document, list := exportFixture(t)
	dbA := filepath.Join(t.TempDir(), "a.db")
	dbB := filepath.Join(t.TempDir(), "b.db")
	primary := startUpstream(t, "203.0.113.10")
	spare := startUpstream(t, "203.0.113.11")

	populateTransferStore(t, dbA, primary, spare, list)
	runAegis(t, "export", "--db", dbA, "-o", document)
	runAegis(t, "import", "--db", dbB, document)

	a := openStoreForTest(t, dbA)
	b := openStoreForTest(t, dbB)

	exported, err := a.ReadDocument(ctx)
	require.NoError(t, err)
	imported, err := b.ReadDocument(ctx)
	require.NoError(t, err)
	require.Equal(t, exported, imported)

	cfg, err := b.Load(ctx)
	require.NoError(t, err)
	require.NoError(t, cfg.Validate())

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	rtA := runtime.New(a, nil, quiet)
	rtB := runtime.New(b, nil, quiet)
	require.NoError(t, rtA.Reload(ctx))
	require.NoError(t, rtB.Reload(ctx))

	tablet := netip.MustParseAddr("10.9.9.2")
	outsider := netip.MustParseAddr("10.9.9.9")
	names := []string{"games.example", "example.com", "x.routed.example", "anything.example.net"}
	for _, name := range names {
		parsed := parseName(t, name)
		require.Equal(t, rtA.Decide(parsed, tablet), rtB.Decide(parsed, tablet), name)
	}

	blocked := parseName(t, "games.example")
	verdict := rtB.Decide(blocked, tablet)
	require.Equal(t, filter.ActionBlock, verdict.Action)
	require.Equal(t, "custom:1", verdict.Match.RuleID)
	plain := parseName(t, "example.com")
	require.Equal(t, filter.Refused, rtB.Decide(plain, tablet).Policy.Mode)
	require.Equal(t, filter.ActionAllow, rtB.Decide(plain, outsider).Action)

	routed := parseName(t, "x.routed.example")
	require.Equal(t, "primary", rtA.Decide(routed, tablet).Route)
	require.Equal(t, "primary", rtB.Decide(routed, tablet).Route)
	for _, rt := range []*runtime.Runtime{rtA, rtB} {
		question := new(mdns.Msg).SetQuestion(mdns.Fqdn("x.routed.example"), mdns.TypeA)
		resp, err := rt.Upstreams().Resolve(ctx, question, "primary")
		require.NoError(t, err)
		require.Equal(t, "203.0.113.10", answerOf(t, resp))
	}

	name := parseName(t, "nas.example")
	recordA, okA := rtA.Lookup(name, netip.Addr{})
	recordB, okB := rtB.Lookup(name, netip.Addr{})
	require.True(t, okA)
	require.True(t, okB)
	require.Equal(t, recordA, recordB)
	require.Equal(t, netip.MustParseAddr("10.9.9.77"), recordB.Addr)

	syncA := runtime.NewSourceSync(a, blocklist.NewFetcher(time.Second), rtA, quiet)
	syncB := runtime.NewSourceSync(b, blocklist.NewFetcher(time.Second), rtB, quiet)
	require.NoError(t, syncA.RefreshSources(ctx))
	require.NoError(t, syncB.RefreshSources(ctx))
	listed := parseName(t, "listed.example.com")
	require.Equal(t, rtA.Decide(listed, tablet), rtB.Decide(listed, tablet))
	require.Equal(t, filter.ActionBlock, rtB.Decide(listed, tablet).Action,
		"the imported file source fetches and blocks like the original")
}

func TestImportStoresALocalListFileAsASource(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "a.db")
	list := filepath.Join(dir, "extra.txt")
	require.NoError(t, os.WriteFile(list, []byte("0.0.0.0 ads.example.com\n"), 0o644))

	runAegis(t, "import", "--db", db, "--source", "mine="+list)
	runAegis(t, "import", "--db", db, "--source", "adbled="+list, "--format", "adblock")

	database := openStoreForTest(t, db)
	sources, err := database.Sources(t.Context())
	require.NoError(t, err)
	require.Equal(t, []store.Source{
		{Name: "adbled", URL: "file://" + list, Format: blocklist.FormatAdBlock, Enabled: true, Body: []byte{}},
		{Name: "mine", URL: "file://" + list, Format: blocklist.FormatHosts, Enabled: true, Body: []byte{}},
	}, sources)
}

func TestImportUpsertsConflictingRowsThroughTheCLI(t *testing.T) {
	ctx := t.Context()
	document, list := exportFixture(t)
	dbA := filepath.Join(t.TempDir(), "a.db")
	dbC := filepath.Join(t.TempDir(), "c.db")
	primary := startUpstream(t, "203.0.113.10")
	spare := startUpstream(t, "203.0.113.11")

	populateTransferStore(t, dbA, primary, spare, list)
	runAegis(t, "export", "--db", dbA, "-o", document)

	c := openStoreForTest(t, dbC)
	require.NoError(t, c.SaveProfile(ctx, filter.ProfileSpec{ID: "kids", Mode: modePtr(filter.NXDomain)}))
	require.NoError(t, c.SaveRewrite(ctx, parseTarget(t, "keep.example", "10.9.9.9")))

	runAegis(t, "import", "--db", dbC, document)

	cfg, err := c.Load(ctx)
	require.NoError(t, err)
	require.NoError(t, cfg.Validate())
	require.Len(t, cfg.Profiles, 2, "the target keeps the rows the document never names")
	for _, profile := range cfg.Profiles {
		if profile.ID == "kids" {
			require.NotNil(t, profile.Mode)
			require.Equal(t, filter.Refused, *profile.Mode)
		}
	}
	require.Len(t, cfg.Rewrites, 2, "the target keeps the rewrites the document never names")
}

func TestImportRefusesAFileWithoutAKnownVersion(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "a.db")
	bad := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(bad, []byte(`{"version": 99}`), 0o644))

	root := newRootCmd()
	root.SetArgs([]string{"import", "--db", db, bad})
	err := root.Execute()
	require.ErrorContains(t, err, bad)
	require.ErrorContains(t, err, "unsupported document version 99")

	require.NoError(t, os.WriteFile(bad, []byte(`{"profiles": []}`), 0o644))
	root = newRootCmd()
	root.SetArgs([]string{"import", "--db", db, bad})
	err = root.Execute()
	require.ErrorContains(t, err, bad)
	require.ErrorContains(t, err, "no version marker")
}
