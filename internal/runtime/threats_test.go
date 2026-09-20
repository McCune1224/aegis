package runtime_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/blocklist"
	"aegis/internal/runtime"
	"aegis/internal/store"
)

type indexRecorder struct {
	published map[string]string
}

func (r *indexRecorder) PublishIndex(domains map[string]string) {
	r.published = domains
}

func TestThreatSyncFetchesFeedsAndPublishesTheIndex(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "feed.txt")
	require.NoError(t, os.WriteFile(path, []byte("malware.example malware\nmail.evil.example c2\n"), 0o644))

	database := openStore(t)
	require.NoError(t, database.SaveThreatFeed(ctx, store.ThreatFeed{
		Name:    "testfeed",
		URL:     "file://" + path,
		Enabled: true,
	}))
	recorder := &indexRecorder{}
	sync := runtime.NewThreatSync(database, blocklist.NewFetcher(time.Second), recorder, quietLogger())

	require.NoError(t, sync.RefreshFeeds(ctx))

	require.Equal(t, map[string]string{
		"malware.example":   "malware",
		"mail.evil.example": "c2",
	}, recorder.published)

	domains, err := database.ThreatDomains(t.Context())
	require.NoError(t, err)
	require.Len(t, domains, 2)
}

func TestThreatSyncSkipsDisabledFeeds(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "feed.txt")
	require.NoError(t, os.WriteFile(path, []byte("malware.example malware\n"), 0o644))

	database := openStore(t)
	require.NoError(t, database.SaveThreatFeed(ctx, store.ThreatFeed{
		Name:    "off",
		URL:     "file://" + path,
		Enabled: false,
	}))
	recorder := &indexRecorder{}
	sync := runtime.NewThreatSync(database, blocklist.NewFetcher(time.Second), recorder, quietLogger())

	require.NoError(t, sync.RefreshFeeds(ctx))
	require.Nil(t, recorder.published)

	domains, err := database.ThreatDomains(t.Context())
	require.NoError(t, err)
	require.Empty(t, domains)
}

func TestThreatSyncRefreshOneRepublishes(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "feed.txt")
	require.NoError(t, os.WriteFile(path, []byte("beacon.example beacon\n"), 0o644))

	database := openStore(t)
	require.NoError(t, database.SaveThreatFeed(ctx, store.ThreatFeed{
		Name:    "one",
		URL:     "file://" + path,
		Enabled: true,
	}))
	recorder := &indexRecorder{}
	sync := runtime.NewThreatSync(database, blocklist.NewFetcher(time.Second), recorder, quietLogger())

	require.NoError(t, sync.RefreshFeed(ctx, "one"))
	require.Equal(t, map[string]string{"beacon.example": "beacon"}, recorder.published)

	require.Error(t, sync.RefreshFeed(ctx, "ghost"))
}
