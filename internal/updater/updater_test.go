package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const fakeBinary = "#!/bin/sh\necho fake aegis\n"

// releaseTag is the one release every fixture publishes.
const releaseTag = "v0.3.0"

// release is one published release served the way GitHub serves it: a
// metadata endpoint naming the tag and the assets, and one endpoint per
// asset. A request without a User-Agent is refused the way the GitHub API
// refuses one, so a check that forgets the header fails every test here.
type release struct {
	base        string
	tag         string
	archives    map[string][]byte
	corrupt     bool
	noChecksums bool
	metaStatus  int
	rateLimited bool
}

func serveRelease(t *testing.T, archiveNames ...string) *release {
	t.Helper()
	rel := &release{tag: releaseTag, archives: map[string][]byte{}}
	for _, name := range archiveNames {
		payload, err := archiveWith(name, map[string]string{
			binaryEntry(name): fakeBinary,
			"LICENSE":         "MIT",
		})
		require.NoError(t, err)
		rel.archives[name] = payload
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/latest", rel.serveLatest)
	mux.HandleFunc("/assets/", rel.serveAsset)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	rel.base = server.URL
	return rel
}

func (r *release) endpoint() string {
	return r.base + "/latest"
}

func (r *release) checker(platform Platform) *Checker {
	return New(Config{
		Endpoint:  r.endpoint(),
		UserAgent: "aegis/test",
		Platform:  platform,
	})
}

// checksums renders the release's checksums.txt from the archives it holds,
// so the sums always match what the asset endpoints serve.
func (r *release) checksums() string {
	var out strings.Builder
	for name, body := range r.archives {
		digest := sha256.Sum256(body)
		sum := hex.EncodeToString(digest[:])
		if r.corrupt {
			sum = "0" + sum[1:]
		}
		fmt.Fprintf(&out, "%s  %s\n", sum, name)
	}
	return out.String()
}

func (r *release) serveLatest(w http.ResponseWriter, req *http.Request) {
	if req.Header.Get("User-Agent") == "" {
		http.Error(w, "Request forbidden", http.StatusForbidden)
		return
	}
	if r.metaStatus != 0 {
		if r.rateLimited {
			w.Header().Set("X-RateLimit-Remaining", "0")
		}
		http.Error(w, "denied", r.metaStatus)
		return
	}

	type asset struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	}
	payload := struct {
		TagName string  `json:"tag_name"`
		Assets  []asset `json:"assets"`
	}{TagName: r.tag}
	for name := range r.archives {
		payload.Assets = append(payload.Assets, asset{Name: name, URL: r.base + "/assets/" + name})
	}
	if !r.noChecksums {
		payload.Assets = append(payload.Assets, asset{
			Name: "checksums.txt",
			URL:  r.base + "/assets/checksums.txt",
		})
	}
	_ = json.NewEncoder(w).Encode(payload)
}

func (r *release) serveAsset(w http.ResponseWriter, req *http.Request) {
	name := strings.TrimPrefix(req.URL.Path, "/assets/")
	if name == "checksums.txt" {
		_, _ = io.WriteString(w, r.checksums())
		return
	}
	body, ok := r.archives[name]
	if !ok {
		http.Error(w, "no such asset", http.StatusNotFound)
		return
	}
	_, _ = w.Write(body)
}

func archiveWith(name string, entries map[string]string) ([]byte, error) {
	var buf bytes.Buffer
	switch {
	case strings.HasSuffix(name, ".tar.gz"):
		gzipWriter := gzip.NewWriter(&buf)
		tarWriter := tar.NewWriter(gzipWriter)
		for entry, content := range entries {
			header := &tar.Header{Name: entry, Mode: 0o755, Size: int64(len(content))}
			if err := tarWriter.WriteHeader(header); err != nil {
				return nil, err
			}
			if _, err := tarWriter.Write([]byte(content)); err != nil {
				return nil, err
			}
		}
		if err := tarWriter.Close(); err != nil {
			return nil, err
		}
		if err := gzipWriter.Close(); err != nil {
			return nil, err
		}
	case strings.HasSuffix(name, ".zip"):
		zipWriter := zip.NewWriter(&buf)
		for entry, content := range entries {
			file, err := zipWriter.Create(entry)
			if err != nil {
				return nil, err
			}
			if _, err := file.Write([]byte(content)); err != nil {
				return nil, err
			}
		}
		if err := zipWriter.Close(); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported archive %q", name)
	}
	return buf.Bytes(), nil
}

func binaryEntry(asset string) string {
	if strings.Contains(asset, "windows") {
		return "aegis.exe"
	}
	return "aegis"
}

func TestCheckOrdersTheRunningBuildAgainstTheNewestRelease(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		current string
		want    State
	}{
		{name: "an older build is behind", current: "0.2.0", want: StateBehind},
		{name: "the released build is up to date", current: "0.3.0", want: StateUpToDate},
		{name: "a build past the release is ahead", current: "0.4.0", want: StateAhead},
		{name: "a leading v does not change the order", current: "v0.3.0", want: StateUpToDate},
		{name: "a snapshot build has no version to order", current: "dev", want: StateUnversioned},
		{name: "a partial version still orders", current: "0.2", want: StateBehind},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rel := serveRelease(t, "aegis_0.3.0_linux_amd64.tar.gz")
			status, err := rel.checker(Platform{GOOS: "linux", GOARCH: "amd64"}).Check(t.Context(), tc.current)
			require.NoError(t, err)
			require.Equal(t, tc.want, status.State)
			require.Equal(t, tc.current, status.Current)
			require.Equal(t, "v0.3.0", status.Release.Tag)
		})
	}
}

func TestCheckPicksTheArchiveForThisPlatform(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		platform Platform
		want     string
	}{
		{name: "linux amd64", platform: Platform{GOOS: "linux", GOARCH: "amd64"}, want: "aegis_0.3.0_linux_amd64.tar.gz"},
		{name: "linux arm64", platform: Platform{GOOS: "linux", GOARCH: "arm64"}, want: "aegis_0.3.0_linux_arm64.tar.gz"},
		{name: "linux armv7", platform: Platform{GOOS: "linux", GOARCH: "arm", GOARM: "7"}, want: "aegis_0.3.0_linux_armv7.tar.gz"},
		{name: "linux armv6", platform: Platform{GOOS: "linux", GOARCH: "arm", GOARM: "6"}, want: "aegis_0.3.0_linux_armv6.tar.gz"},
		{name: "darwin arm64", platform: Platform{GOOS: "darwin", GOARCH: "arm64"}, want: "aegis_0.3.0_darwin_arm64.tar.gz"},
		{name: "windows amd64", platform: Platform{GOOS: "windows", GOARCH: "amd64"}, want: "aegis_0.3.0_windows_amd64.zip"},
		{name: "a platform the release does not build", platform: Platform{GOOS: "linux", GOARCH: "mips"}, want: ""},
	}
	rel := serveRelease(t,
		"aegis_0.3.0_linux_amd64.tar.gz",
		"aegis_0.3.0_linux_arm64.tar.gz",
		"aegis_0.3.0_linux_armv6.tar.gz",
		"aegis_0.3.0_linux_armv7.tar.gz",
		"aegis_0.3.0_darwin_arm64.tar.gz",
		"aegis_0.3.0_windows_amd64.zip",
	)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			status, err := rel.checker(tc.platform).Check(t.Context(), "0.2.0")
			require.NoError(t, err)
			require.Equal(t, tc.want, status.Archive.Name)
		})
	}
}

func TestCheckNamesTheRateLimitWhenGitHubRefuses(t *testing.T) {
	t.Parallel()
	rel := serveRelease(t, "aegis_0.3.0_linux_amd64.tar.gz")
	rel.metaStatus = http.StatusForbidden
	rel.rateLimited = true

	_, err := rel.checker(Platform{GOOS: "linux", GOARCH: "amd64"}).Check(t.Context(), "0.2.0")
	require.ErrorContains(t, err, "rate limit")
}

func TestCheckExplainsAMissingRelease(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Not Found", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	checker := New(Config{
		Endpoint:  server.URL + "/latest",
		UserAgent: "aegis/test",
		Platform:  Platform{GOOS: "linux", GOARCH: "amd64"},
	})
	_, err := checker.Check(t.Context(), "0.2.0")
	require.ErrorContains(t, err, "404")
}

func TestInstallReplacesTheRunningBinary(t *testing.T) {
	t.Parallel()
	rel := serveRelease(t, "aegis_0.3.0_linux_arm64.tar.gz")
	target := filepath.Join(t.TempDir(), "aegis")
	require.NoError(t, os.WriteFile(target, []byte("old build"), 0o755))

	checker := rel.checker(Platform{GOOS: "linux", GOARCH: "arm64"})
	status, err := checker.Check(t.Context(), "0.2.0")
	require.NoError(t, err)
	require.NoError(t, checker.Install(t.Context(), status, target))

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, fakeBinary, string(got))

	info, err := os.Stat(target)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o755), info.Mode().Perm())
	require.Equal(t, StateBehind, status.State)
	require.Equal(t, "aegis_0.3.0_linux_arm64.tar.gz", status.Archive.Name)
}

func TestInstallReplacesTheRunningBinaryFromAZipAsset(t *testing.T) {
	t.Parallel()
	rel := serveRelease(t, "aegis_0.3.0_windows_amd64.zip")
	target := filepath.Join(t.TempDir(), "aegis.exe")
	require.NoError(t, os.WriteFile(target, []byte("old build"), 0o755))

	checker := rel.checker(Platform{GOOS: "windows", GOARCH: "amd64"})
	status, err := checker.Check(t.Context(), "0.2.0")
	require.NoError(t, err)
	require.NoError(t, checker.Install(t.Context(), status, target))

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, fakeBinary, string(got))
}

func TestInstallRefusesAChecksumThatDoesNotMatch(t *testing.T) {
	t.Parallel()
	rel := serveRelease(t, "aegis_0.3.0_linux_amd64.tar.gz")
	rel.corrupt = true
	target := filepath.Join(t.TempDir(), "aegis")
	require.NoError(t, os.WriteFile(target, []byte("old build"), 0o755))

	checker := rel.checker(Platform{GOOS: "linux", GOARCH: "amd64"})
	status, err := checker.Check(t.Context(), "0.2.0")
	require.NoError(t, err)
	require.ErrorContains(t, checker.Install(t.Context(), status, target), "checksum")

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "old build", string(got), "a refused download must leave the running binary alone")
}

func TestInstallRefusesAReleaseWithoutChecksums(t *testing.T) {
	t.Parallel()
	rel := serveRelease(t, "aegis_0.3.0_linux_amd64.tar.gz")
	rel.noChecksums = true
	target := filepath.Join(t.TempDir(), "aegis")

	checker := rel.checker(Platform{GOOS: "linux", GOARCH: "amd64"})
	status, err := checker.Check(t.Context(), "0.2.0")
	require.NoError(t, err)
	require.ErrorContains(t, checker.Install(t.Context(), status, target), "checksums.txt")
}

func TestInstallRefusesWhenThePlatformHasNoArchive(t *testing.T) {
	t.Parallel()
	rel := serveRelease(t, "aegis_0.3.0_linux_amd64.tar.gz")
	target := filepath.Join(t.TempDir(), "aegis")

	checker := rel.checker(Platform{GOOS: "linux", GOARCH: "mips"})
	status, err := checker.Check(t.Context(), "0.2.0")
	require.NoError(t, err)
	require.ErrorContains(t, checker.Install(t.Context(), status, target), "no asset for linux/mips")
}

func TestInstallRefusesAnArchiveWithoutTheBinary(t *testing.T) {
	t.Parallel()
	rel := serveRelease(t, "aegis_0.3.0_linux_amd64.tar.gz")
	payload, err := archiveWith("aegis_0.3.0_linux_amd64.tar.gz", map[string]string{"LICENSE": "MIT"})
	require.NoError(t, err)
	rel.archives["aegis_0.3.0_linux_amd64.tar.gz"] = payload
	target := filepath.Join(t.TempDir(), "aegis")

	checker := rel.checker(Platform{GOOS: "linux", GOARCH: "amd64"})
	status, err := checker.Check(t.Context(), "0.2.0")
	require.NoError(t, err)
	require.ErrorContains(t, checker.Install(t.Context(), status, target), "binary")
}
