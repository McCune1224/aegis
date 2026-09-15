package runtime_test

import (
	"io"
	"log/slog"
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
	require.NoError(t, database.RecordSourceFetch(ctx, "cached", "etag", time.Now(), nil, 1, []byte("0.0.0.0 ads.example.com\n")))

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
	require.NoError(t, database.RecordSourceFetch(ctx, "off", "", time.Now(), nil, 1, []byte("0.0.0.0 ads.example.com\n")))

	engine := runtime.New(database, nil, quietLogger())
	sync := runtime.NewSourceSync(database, blocklist.NewFetcher(2*time.Second), engine, quietLogger())

	require.NoError(t, sync.RefreshSources(ctx))

	require.Equal(t, filter.ActionAllow, engine.Decide(blockedName, tablet).Action)
	require.Equal(t, 0, engine.Size())
}

func mustDomain(t *testing.T, name string) filter.Domain {
	t.Helper()
	domain, err := filter.ParseDomain(name)
	require.NoError(t, err)
	return domain
}
