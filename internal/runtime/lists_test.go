package runtime_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/blocklist"
	"aegis/internal/filter"
	"aegis/internal/runtime"
	"aegis/internal/store"
)

func writeList(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "list.txt")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	return path
}

// requireSameSize states the precondition the two cache tests share: a rewrite
// whose content is a different name of the same length.
func requireSameSize(t *testing.T, before os.FileInfo, path string) {
	t.Helper()
	after, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, before.Size(), after.Size(), "the rewrite keeps the file size")
}

func listRuntime(t *testing.T, path string) *runtime.Runtime {
	t.Helper()
	rt := runtime.New(openStore(t), []runtime.ListFile{{Path: path, Format: blocklist.FormatHosts}}, quietLogger())
	require.NoError(t, rt.Reload(t.Context()))
	return rt
}

func TestReloadPicksUpAnEditedBlocklistFile(t *testing.T) {
	path := writeList(t, "ads.example.com")
	rt := listRuntime(t, path)

	require.Equal(t, filter.ActionBlock, rt.Decide(blockedName, tablet).Action)

	other := mustDomain(t, "tracker.example.com")
	require.NoError(t, os.WriteFile(path, []byte("tracker.example.com\n"), 0o600))
	require.NoError(t, rt.Reload(t.Context()))

	require.Equal(t, filter.ActionAllow, rt.Decide(blockedName, tablet).Action)
	require.Equal(t, filter.ActionBlock, rt.Decide(other, tablet).Action)
}

// TestReloadNoticesAnEditThatKeepsTheFileSize covers the half of the cache's key
// that a same-length rewrite would hide if the size were compared alone.
func TestReloadNoticesAnEditThatKeepsTheFileSize(t *testing.T) {
	path := writeList(t, "ads.example.com")
	info, err := os.Stat(path)
	require.NoError(t, err)
	rt := listRuntime(t, path)

	other := mustDomain(t, "new.example.com")
	require.NoError(t, os.WriteFile(path, []byte("new.example.com\n"), 0o600))
	requireSameSize(t, info, path)
	later := info.ModTime().Add(time.Second)
	require.NoError(t, os.Chtimes(path, later, later))
	require.NoError(t, rt.Reload(t.Context()))

	require.Equal(t, filter.ActionBlock, rt.Decide(other, tablet).Action)
	require.Equal(t, filter.ActionAllow, rt.Decide(blockedName, tablet).Action)
}

// TestAReloadReusesAFileWhoseFormDidNotChange covers what the cache is for, a
// reload after a configuration write that reuses a file nobody touched. A rewrite
// preserving both the size and the modification time is invisible to a stat, and
// this is that case.
func TestAReloadReusesAFileWhoseFormDidNotChange(t *testing.T) {
	path := writeList(t, "ads.example.com")
	info, err := os.Stat(path)
	require.NoError(t, err)
	rt := listRuntime(t, path)

	other := mustDomain(t, "new.example.com")
	require.NoError(t, os.WriteFile(path, []byte("new.example.com\n"), 0o600))
	requireSameSize(t, info, path)
	require.NoError(t, os.Chtimes(path, info.ModTime(), info.ModTime()))
	require.NoError(t, rt.Reload(t.Context()))

	require.Equal(t, filter.ActionBlock, rt.Decide(blockedName, tablet).Action, "the rules already read keep serving")
	require.Equal(t, filter.ActionAllow, rt.Decide(other, tablet).Action)
}

func TestReloadKeepsServingWhenAListFileVanishes(t *testing.T) {
	path := writeList(t, "ads.example.com")
	rt := listRuntime(t, path)

	require.NoError(t, os.Remove(path))
	require.Error(t, rt.Reload(t.Context()))

	require.Equal(t, filter.ActionBlock, rt.Decide(blockedName, tablet).Action)
}

// BenchmarkReloadWithALargeList measures the reload that follows a configuration
// write. The first reload reads and parses the file; the ones after it reuse the
// parse and pay only the rule compile.
func BenchmarkReloadWithALargeList(b *testing.B) {
	const lines = 250000
	var body strings.Builder
	for i := range lines {
		body.WriteString("tracker" + strconv.Itoa(i) + ".example.com\n")
	}
	path := filepath.Join(b.TempDir(), "list.txt")
	require.NoError(b, os.WriteFile(path, []byte(body.String()), 0o600))

	database, err := store.Open(b.Context(), filepath.Join(b.TempDir(), "aegis.db"))
	require.NoError(b, err)
	b.Cleanup(func() { _ = database.Close() })
	require.NoError(b, database.SaveUpstream(b.Context(), store.Upstream{Name: deadUpstream, URL: deadUpstream, Enabled: true}))

	rt := runtime.New(database, []runtime.ListFile{{Path: path, Format: blocklist.FormatHosts}}, quietLogger())
	require.NoError(b, rt.Reload(b.Context()))
	require.Equal(b, lines, rt.Size())

	b.ReportAllocs()
	for b.Loop() {
		if err := rt.Reload(b.Context()); err != nil {
			b.Fatal(err)
		}
	}
}
