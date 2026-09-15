package runtime_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/blocklist"
	"aegis/internal/filter"
	"aegis/internal/runtime"
)

func writeList(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "list.txt")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	return path
}

func TestReloadReadsTheBlocklistFilesAgain(t *testing.T) {
	path := writeList(t, "ads.example.com")
	rt := runtime.New(openStore(t), []runtime.ListFile{{Path: path, Format: blocklist.FormatHosts}}, quietLogger())
	require.NoError(t, rt.Reload(t.Context()))

	require.Equal(t, filter.ActionBlock, rt.Decide(blockedName, tablet).Action)

	other, err := filter.ParseDomain("tracker.example.com")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("tracker.example.com\n"), 0o600))
	require.NoError(t, rt.Reload(t.Context()))

	require.Equal(t, filter.ActionAllow, rt.Decide(blockedName, tablet).Action)
	require.Equal(t, filter.ActionBlock, rt.Decide(other, tablet).Action)
}

func TestReloadKeepsServingWhenAListFileVanishes(t *testing.T) {
	path := writeList(t, "ads.example.com")
	rt := runtime.New(openStore(t), []runtime.ListFile{{Path: path, Format: blocklist.FormatHosts}}, quietLogger())
	require.NoError(t, rt.Reload(t.Context()))

	require.NoError(t, os.Remove(path))
	require.Error(t, rt.Reload(t.Context()))

	require.Equal(t, filter.ActionBlock, rt.Decide(blockedName, tablet).Action)
}
