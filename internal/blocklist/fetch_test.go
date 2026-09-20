package blocklist_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/blocklist"
	"aegis/internal/filter"
)

func listServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestFetchReadsABodyAndItsETag(t *testing.T) {
	server := listServer(t, "0.0.0.0 ads.example.com\n")

	result, err := blocklist.NewFetcher(5*time.Second).Fetch(context.Background(), server.URL, "")

	require.NoError(t, err)
	require.False(t, result.NotModified)
	require.Equal(t, `"v1"`, result.ETag)
	require.Contains(t, string(result.Body), "ads.example.com")
}

func TestFetchIsConditional(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = io.WriteString(w, "0.0.0.0 ads.example.com\n")
	}))
	t.Cleanup(server.Close)

	result, err := blocklist.NewFetcher(5*time.Second).Fetch(context.Background(), server.URL, `"v1"`)

	require.NoError(t, err)
	require.True(t, result.NotModified)
	require.Empty(t, result.Body)
}

func TestFetchReportsABadStatusAndAnUnreachableHost(t *testing.T) {
	missing := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(missing.Close)
	fetcher := blocklist.NewFetcher(2 * time.Second)

	_, err := fetcher.Fetch(context.Background(), missing.URL, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "404")

	_, err = fetcher.Fetch(context.Background(), "http://127.0.0.1:1/hosts", "")
	require.Error(t, err)
}

func TestFetchedBodyParsesIntoRules(t *testing.T) {
	server := listServer(t, "0.0.0.0 ads.example.com\n")

	fetched, err := blocklist.NewFetcher(5*time.Second).Fetch(context.Background(), server.URL, "")
	require.NoError(t, err)
	source := filter.Source{ID: "stevenblack", Name: "StevenBlack"}
	result, err := blocklist.ParseList(strings.NewReader(string(fetched.Body)), source, blocklist.FormatHosts)

	require.NoError(t, err)
	require.Len(t, result.Rules, 1)
	require.Equal(t, "ads.example.com", result.Rules[0].Domain.String())
	require.Equal(t, filter.ActionBlock, result.Rules[0].Action)
}

func TestFetchReadsAFileSourceConditionally(t *testing.T) {
	path := filepath.Join(t.TempDir(), "list.txt")
	require.NoError(t, os.WriteFile(path, []byte("0.0.0.0 ads.example.com\n"), 0o644))
	fetcher := blocklist.NewFetcher(5 * time.Second)

	first, err := fetcher.Fetch(context.Background(), "file://"+path, "")

	require.NoError(t, err)
	require.False(t, first.NotModified)
	require.Contains(t, string(first.Body), "ads.example.com")
	require.NotEmpty(t, first.ETag)

	second, err := fetcher.Fetch(context.Background(), "file://"+path, first.ETag)

	require.NoError(t, err)
	require.True(t, second.NotModified, "an unchanged file answers 304 like a remote source")
	require.Empty(t, second.Body)

	require.NoError(t, os.WriteFile(path, []byte("0.0.0.0 ads.example.com\n0.0.0.0 more.example\n"), 0o644))
	third, err := fetcher.Fetch(context.Background(), "file://"+path, first.ETag)

	require.NoError(t, err)
	require.False(t, third.NotModified)
	require.Contains(t, string(third.Body), "more.example")
	require.NotEqual(t, first.ETag, third.ETag)
}

func TestFetchOfAMissingFileIsAnError(t *testing.T) {
	_, err := blocklist.NewFetcher(5*time.Second).Fetch(context.Background(), "file:///no/such/list.txt", "")

	require.Error(t, err)
	require.Contains(t, err.Error(), "no such file")
}

func TestTheCatalogIsWellFormed(t *testing.T) {
	require.NotEmpty(t, blocklist.Catalog)
	for _, entry := range blocklist.Catalog {
		require.NotEmpty(t, entry.Name, "catalog entry has no name")
		require.True(t, strings.HasPrefix(entry.URL, "https://"), "catalog entry %q is not https", entry.Name)
		_, err := blocklist.ParseFormat(entry.Format.String())
		require.NoError(t, err, "catalog entry %q has a bad format", entry.Name)
	}
}
