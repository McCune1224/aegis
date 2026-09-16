package blocklist_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/blocklist"
	"aegis/internal/filter"
)

func TestDiffListsReportsTheExactDomainChanges(t *testing.T) {
	source := filter.Source{ID: "test", Name: "Test"}
	oldBody := strings.NewReader("ads.example.com\ntracker.example.net\nallowed.example.com\n")
	newBody := strings.NewReader("tracker.example.net\nallowed.example.com\nmalware.example.net\n")

	diff, err := blocklist.DiffLists(oldBody, newBody, source, blocklist.FormatHosts)
	require.NoError(t, err)

	require.True(t, diff.Changed())
	require.Equal(t, []string{"malware.example.net"}, diff.Added)
	require.Equal(t, []string{"ads.example.com"}, diff.Removed)
}

func TestDiffListsReportsNoChangeForIdenticalBodies(t *testing.T) {
	source := filter.Source{ID: "test", Name: "Test"}
	text := "ads.example.com\n# a comment line\n"

	diff, err := blocklist.DiffLists(strings.NewReader(text), strings.NewReader(text), source, blocklist.FormatHosts)
	require.NoError(t, err)
	require.False(t, diff.Changed())
	require.Empty(t, diff.Added)
	require.Empty(t, diff.Removed)
}
