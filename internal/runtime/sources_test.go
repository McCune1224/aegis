package runtime_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/blocklist"
	"aegis/internal/filter"
	"aegis/internal/runtime"
	"aegis/internal/store"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// RefreshFallsBackToTheCachedBody covers the boot path and the API write path
// at once: a source whose URL is down keeps serving its last good body, and the
// failure is recorded on the source rather than ending the refresh.
func TestRefreshFallsBackToTheCachedBody(t *testing.T) {
	database := openStore(t)
	ctx := t.Context()

	require.NoError(t, database.SaveSource(ctx, store.Source{
		Name:    "cached",
		URL:     "http://127.0.0.1:1/list",
		Format:  blocklist.FormatHosts,
		Enabled: true,
	}))
	require.NoError(t, database.RecordSourceFetch(ctx, "cached", "etag", time.Now(), nil, 1, 0, []byte("0.0.0.0 ads.example.com\n")))

	engine := runtime.New(database, nil, quietLogger())
	sync := runtime.NewSourceSync(database, blocklist.NewFetcher(2*time.Second), engine, quietLogger())

	require.NoError(t, sync.RefreshSources(ctx))

	require.Equal(t, filter.ActionBlock, engine.Decide(blockedName, tablet).Action)
	require.Equal(t, filter.ActionAllow, engine.Decide(mustDomain(t, "example.com"), tablet).Action)

	sources, err := database.Sources(ctx)
	require.NoError(t, err)
	require.Len(t, sources, 1)
	require.Contains(t, sources[0].LastError, "fetch")
	require.Equal(t, 1, sources[0].RuleCount)
}

// A disabled source is invisible to a refresh, which is what makes the enable
// toggle able to take rules out of the engine.
func TestRefreshSkipsDisabledSources(t *testing.T) {
	database := openStore(t)
	ctx := t.Context()

	require.NoError(t, database.SaveSource(ctx, store.Source{
		Name:    "off",
		URL:     "http://127.0.0.1:1/list",
		Format:  blocklist.FormatHosts,
		Enabled: false,
	}))
	require.NoError(t, database.RecordSourceFetch(ctx, "off", "", time.Now(), nil, 1, 0, []byte("0.0.0.0 ads.example.com\n")))

	engine := runtime.New(database, nil, quietLogger())
	sync := runtime.NewSourceSync(database, blocklist.NewFetcher(2*time.Second), engine, quietLogger())

	require.NoError(t, sync.RefreshSources(ctx))

	require.Equal(t, filter.ActionAllow, engine.Decide(blockedName, tablet).Action)
	require.Equal(t, 0, engine.Size())
}

// listServer serves one mutable hosts list and counts how often it was
// fetched, so tests can tell a skipped refresh from a real one.
type listServer struct {
	*httptest.Server
	fetches *atomic.Int64
}

func newListServer(t *testing.T) listServer {
	t.Helper()
	var fetches atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		_, _ = w.Write([]byte("0.0.0.0 ads.example.com\n"))
	}))
	t.Cleanup(server.Close)
	return listServer{Server: server, fetches: &fetches}
}

func (l listServer) setBody(body string) {
	l.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		l.fetches.Add(1)
		_, _ = w.Write([]byte(body))
	})
}

func mustDomain(t *testing.T, name string) filter.Domain {
	t.Helper()
	domain, err := filter.ParseDomain(name)
	require.NoError(t, err)
	return domain
}

func TestRefreshOneAppliesChangedContentWithoutARestart(t *testing.T) {
	database := openStore(t)
	ctx := t.Context()

	list := newListServer(t)
	require.NoError(t, database.SaveSource(ctx, store.Source{Name: "live", URL: list.URL, Format: blocklist.FormatHosts, Enabled: true}))

	engine := runtime.New(database, nil, quietLogger())
	sync := runtime.NewSourceSync(database, blocklist.NewFetcher(2*time.Second), engine, quietLogger())
	require.NoError(t, sync.RefreshSources(ctx))
	require.Equal(t, filter.ActionBlock, engine.Decide(blockedName, tablet).Action)

	list.setBody("0.0.0.0 tracker.example.net\n")
	require.NoError(t, sync.RefreshOne(ctx, "live"))

	require.Equal(t, filter.ActionAllow, engine.Decide(blockedName, tablet).Action)
	require.Equal(t, filter.ActionBlock, engine.Decide(mustDomain(t, "tracker.example.net"), tablet).Action)
}

func TestPreviewReportsChangesWithoutApplyingThem(t *testing.T) {
	database := openStore(t)
	ctx := t.Context()

	list := newListServer(t)
	require.NoError(t, database.SaveSource(ctx, store.Source{Name: "live", URL: list.URL, Format: blocklist.FormatHosts, Enabled: true}))

	engine := runtime.New(database, nil, quietLogger())
	sync := runtime.NewSourceSync(database, blocklist.NewFetcher(2*time.Second), engine, quietLogger())
	require.NoError(t, sync.RefreshSources(ctx))
	require.Equal(t, filter.ActionBlock, engine.Decide(blockedName, tablet).Action)

	list.setBody("0.0.0.0 tracker.example.net\n")
	added, removed, notModified, err := sync.PreviewSource(ctx, "live")
	require.NoError(t, err)
	require.False(t, notModified)
	require.Equal(t, []string{"tracker.example.net"}, added)
	require.Equal(t, []string{"ads.example.com"}, removed)

	require.Equal(t, filter.ActionBlock, engine.Decide(blockedName, tablet).Action,
		"a preview must not change what the resolver answers")
	stored, err := database.Sources(ctx)
	require.NoError(t, err)
	require.Contains(t, string(stored[0].Body), "ads.example.com")

	require.NoError(t, sync.RefreshOne(ctx, "live"))
	require.Equal(t, filter.ActionBlock, engine.Decide(mustDomain(t, "tracker.example.net"), tablet).Action)
}

func TestRefreshDueSkipsFreshSourcesAndFetchesStaleOnes(t *testing.T) {
	database := openStore(t)
	ctx := t.Context()

	list := newListServer(t)
	require.NoError(t, database.SaveSource(ctx, store.Source{Name: "due", URL: list.URL, Format: blocklist.FormatHosts, Enabled: true, RefreshSeconds: 3600}))

	engine := runtime.New(database, nil, quietLogger())
	sync := runtime.NewSourceSync(database, blocklist.NewFetcher(2*time.Second), engine, quietLogger())
	require.NoError(t, sync.RefreshSources(ctx))
	firstFetches := list.fetches.Load()

	now := time.Now()
	fetched, err := sync.RefreshDue(ctx, now.Add(time.Minute), time.Hour)
	require.NoError(t, err)
	require.Zero(t, fetched, "a source fetched a minute ago is not due")
	require.Equal(t, firstFetches, list.fetches.Load())

	fetched, err = sync.RefreshDue(ctx, now.Add(2*time.Hour), time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, fetched)
	require.Greater(t, list.fetches.Load(), firstFetches)
}

func TestConsecutiveFailuresCountAndClear(t *testing.T) {
	database := openStore(t)
	ctx := t.Context()

	require.NoError(t, database.SaveSource(ctx, store.Source{Name: "flaky", URL: "http://127.0.0.1:1/list", Format: blocklist.FormatHosts, Enabled: true}))

	engine := runtime.New(database, nil, quietLogger())
	sync := runtime.NewSourceSync(database, blocklist.NewFetcher(time.Second), engine, quietLogger())

	for range 3 {
		require.NoError(t, sync.RefreshSources(ctx))
	}
	sources, err := database.Sources(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, sources[0].Failures)

	list := newListServer(t)
	require.NoError(t, database.SaveSource(ctx, store.Source{Name: "flaky", URL: list.URL, Format: blocklist.FormatHosts, Enabled: true}))
	require.NoError(t, sync.RefreshSources(ctx))

	sources, err = database.Sources(ctx)
	require.NoError(t, err)
	require.Zero(t, sources[0].Failures, "a good fetch clears the failure streak")
}

func TestARestartedSyncRepublishesTheStoredListAfterA304(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "list.txt")
	require.NoError(t, os.WriteFile(path, []byte("0.0.0.0 listed.example.com\n"), 0o644))

	database := openStore(t)
	require.NoError(t, database.SaveSource(ctx, store.Source{
		Name:    "local",
		URL:     "file://" + path,
		Format:  blocklist.FormatHosts,
		Enabled: true,
	}))

	booted := runtime.New(database, nil, quietLogger())
	first := runtime.NewSourceSync(database, blocklist.NewFetcher(time.Second), booted, quietLogger())
	require.NoError(t, first.RefreshSources(ctx))

	listed, err := filter.ParseDomain("listed.example.com")
	require.NoError(t, err)
	require.Equal(t, filter.ActionBlock, booted.Decide(listed, netip.Addr{}).Action)

	// A second sync reads the same stored source: the etag matches, the fetch
	// answers 304, and the stored list must keep serving.
	second := runtime.New(database, nil, quietLogger())
	restarted := runtime.NewSourceSync(database, blocklist.NewFetcher(time.Second), second, quietLogger())
	require.NoError(t, restarted.RefreshSources(ctx))
	require.Equal(t, filter.ActionBlock, second.Decide(listed, netip.Addr{}).Action,
		"a 304 must republish the stored list, not empty the source rules")
}
