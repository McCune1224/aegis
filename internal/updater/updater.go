// Package updater checks the published releases for a newer build and
// replaces the running binary with it.
package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// metadataTimeout bounds the two small requests, the release metadata
	// and the checksums.
	metadataTimeout = 30 * time.Second

	// downloadTimeout bounds the archive fetch, which is several megabytes
	// and may cross a slow link.
	downloadTimeout = 5 * time.Minute

	// maxMetadataBytes caps a metadata response, and maxAssetBytes caps a
	// downloaded archive, so a broken or hostile release cannot fill memory
	// or disk.
	maxMetadataBytes = 4 << 20
	maxAssetBytes    = 64 << 20

	// maxRedirects is how many hops a fetch follows before it gives up.
	maxRedirects = 5

	// checksumsName is the file every release publishes its sums in.
	checksumsName = "checksums.txt"

	// executableName is what the archives call the binary.
	executableName = "aegis"
)

// State is where the running build stands against the newest release.
type State string

const (
	// StateUpToDate means the running build is the newest release.
	StateUpToDate State = "up-to-date"
	// StateBehind means a newer release exists.
	StateBehind State = "behind"
	// StateAhead means the running build is past the newest release.
	StateAhead State = "ahead"
	// StateUnversioned means the running build carries no version that can
	// be ordered against a release tag.
	StateUnversioned State = "unversioned"
)

// Platform is the machine the running build was compiled for.
type Platform struct {
	GOOS   string
	GOARCH string
	GOARM  string
}

// Config is what a Checker needs in order to find releases and pick the one
// that fits this machine.
type Config struct {
	Endpoint  string
	UserAgent string
	Platform  Platform
}

// Release is the newest published release.
type Release struct {
	Tag    string
	Assets []Asset
}

// Asset is one file a release ships.
type Asset struct {
	Name string
	URL  string
}

// Status is one comparison of the running build with the newest release.
type Status struct {
	State   State
	Current string
	Release Release
	Archive Asset
}

// Checker reads a release index and installs the release archive for one
// platform over a target binary.
type Checker struct {
	endpoint  string
	userAgent string
	platform  Platform
	client    *http.Client
}

// New returns a Checker for the releases/latest endpoint in cfg. The HTTP
// client bounds every request, follows few redirects, and refuses a redirect
// off the http schemes.
func New(cfg Config) *Checker {
	return &Checker{
		endpoint:  cfg.Endpoint,
		userAgent: cfg.UserAgent,
		platform:  cfg.Platform,
		client: &http.Client{
			Timeout: downloadTimeout,
			CheckRedirect: func(request *http.Request, via []*http.Request) error {
				if len(via) >= maxRedirects {
					return fmt.Errorf("updater: too many redirects")
				}
				if request.URL.Scheme != "http" && request.URL.Scheme != "https" {
					return fmt.Errorf("updater: redirect to unsupported scheme %q", request.URL.Scheme)
				}
				return nil
			},
		},
	}
}

// Check reports where current stands against the newest release and names the
// release archive for this platform, which is empty when the release ships
// none.
func (c *Checker) Check(ctx context.Context, current string) (Status, error) {
	release, err := c.latest(ctx)
	if err != nil {
		return Status{}, err
	}
	return Status{
		State:   order(current, release.Tag),
		Current: current,
		Release: release,
		Archive: pick(release.Assets, c.platform),
	}, nil
}

// Install downloads the archive status names, checks it against the release
// checksums, and replaces target with the binary inside it. Any refusal
// leaves target as it was.
func (c *Checker) Install(ctx context.Context, status Status, target string) error {
	if status.Archive.Name == "" {
		return fmt.Errorf("updater: %s ships no asset for %s", status.Release.Tag, c.platform.label())
	}

	want, err := c.checksumFor(ctx, status.Release, status.Archive.Name)
	if err != nil {
		return err
	}

	staged, got, cleanup, err := c.download(ctx, status.Archive)
	if err != nil {
		return err
	}
	defer cleanup()
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("updater: checksum mismatch for %s, the release says %s and the download gave %s",
			status.Archive.Name, want, got)
	}

	binary, err := extract(staged, c.platform)
	if err != nil {
		return err
	}
	return replace(target, binary)
}

// latest reads the newest release and its assets.
func (c *Checker) latest(ctx context.Context) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()

	body, err := c.get(ctx, c.endpoint, "release metadata")
	if err != nil {
		return Release{}, err
	}

	var payload struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Release{}, fmt.Errorf("updater: release metadata: %w", err)
	}
	if payload.TagName == "" {
		return Release{}, fmt.Errorf("updater: release metadata names no tag")
	}

	release := Release{Tag: payload.TagName}
	for _, asset := range payload.Assets {
		release.Assets = append(release.Assets, Asset{Name: asset.Name, URL: asset.URL})
	}
	return release, nil
}

// checksumFor reads the release's checksums file and returns the sum it
// records for asset.
func (c *Checker) checksumFor(ctx context.Context, release Release, asset string) (string, error) {
	var url string
	for _, candidate := range release.Assets {
		if candidate.Name == checksumsName {
			url = candidate.URL
			break
		}
	}
	if url == "" {
		return "", fmt.Errorf("updater: %s ships no %s", release.Tag, checksumsName)
	}

	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	body, err := c.get(ctx, url, checksumsName)
	if err != nil {
		return "", err
	}

	sums, err := parseChecksums(body)
	if err != nil {
		return "", err
	}
	sum, ok := sums[asset]
	if !ok {
		return "", fmt.Errorf("updater: %s does not list %s", checksumsName, asset)
	}
	return sum, nil
}

// get reads one small metadata body. GitHub refuses a request with no
// User-Agent, and a spent quota answers 403, so both are named in the error.
func (c *Checker) get(ctx context.Context, url, what string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("updater: %s: %w", what, err)
	}
	request.Header.Set("User-Agent", c.userAgent)

	response, err := c.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("updater: %s: %w", what, err)
	}
	defer func() { _ = response.Body.Close() }()

	switch {
	case response.StatusCode == http.StatusOK:
	case response.StatusCode == http.StatusForbidden && response.Header.Get("X-RateLimit-Remaining") == "0":
		return nil, fmt.Errorf("updater: %s: GitHub rate limit reached, try again later", what)
	default:
		return nil, fmt.Errorf("updater: %s: status %s", what, response.Status)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxMetadataBytes+1))
	if err != nil {
		return nil, fmt.Errorf("updater: %s: %w", what, err)
	}
	if len(body) > maxMetadataBytes {
		return nil, fmt.Errorf("updater: %s: response is larger than the size cap", what)
	}
	return body, nil
}

// download saves asset next to the temporary files and returns its path, the
// sha256 of what arrived, and a cleanup that removes it.
func (c *Checker) download(ctx context.Context, asset Asset) (path, sum string, cleanup func(), err error) {
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return "", "", nil, fmt.Errorf("updater: download %s: %w", asset.Name, err)
	}
	request.Header.Set("User-Agent", c.userAgent)

	response, err := c.client.Do(request)
	if err != nil {
		return "", "", nil, fmt.Errorf("updater: download %s: %w", asset.Name, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", "", nil, fmt.Errorf("updater: download %s: status %s", asset.Name, response.Status)
	}

	file, err := os.CreateTemp("", "aegis-release-*")
	if err != nil {
		return "", "", nil, fmt.Errorf("updater: stage the download: %w", err)
	}
	staged := file.Name()
	remove := func() { _ = os.Remove(staged) }

	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, digest), io.LimitReader(response.Body, maxAssetBytes+1))
	if err != nil {
		_ = file.Close()
		remove()
		return "", "", nil, fmt.Errorf("updater: download %s: %w", asset.Name, err)
	}
	if written > maxAssetBytes {
		_ = file.Close()
		remove()
		return "", "", nil, fmt.Errorf("updater: download %s: archive is larger than the size cap", asset.Name)
	}
	if err := file.Close(); err != nil {
		remove()
		return "", "", nil, fmt.Errorf("updater: stage the download: %w", err)
	}
	return staged, hex.EncodeToString(digest.Sum(nil)), remove, nil
}

// parseChecksums reads a sha256sum-format body, one "<sum>  <name>" line per
// asset.
func parseChecksums(body []byte) (map[string]string, error) {
	sums := map[string]string{}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || len(fields[0]) != sha256.Size*2 {
			continue
		}
		sums[strings.TrimPrefix(fields[1], "*")] = strings.ToLower(fields[0])
	}
	if len(sums) == 0 {
		return nil, fmt.Errorf("updater: %s lists no checksums", checksumsName)
	}
	return sums, nil
}

// pick returns the asset published for this platform, or an empty Asset when
// the release ships none.
func pick(assets []Asset, platform Platform) Asset {
	token := "_" + platform.GOOS + "_" + platform.arch() + platform.extension()
	for _, asset := range assets {
		if strings.Contains(asset.Name, token) {
			return asset
		}
	}
	return Asset{}
}

// order compares the running build with a release tag. A build that carries
// no comparable version is unversioned rather than ordered, so a snapshot
// cannot be silently replaced.
func order(current, tag string) State {
	running, runningOK := parseVersion(current)
	published, publishedOK := parseVersion(tag)
	if !runningOK || !publishedOK {
		return StateUnversioned
	}
	switch cmp := compare(running, published); {
	case cmp < 0:
		return StateBehind
	case cmp > 0:
		return StateAhead
	default:
		return StateUpToDate
	}
}

type version [3]int

// parseVersion reads "1", "1.2", "1.2.3", or a release tag carrying a "v".
// Anything else, a suffix or a snapshot label included, has no order.
func parseVersion(raw string) (version, bool) {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(raw, "v"), "V")
	if trimmed == "" {
		return version{}, false
	}
	var parsed version
	fields := strings.Split(trimmed, ".")
	if len(fields) > len(parsed) {
		return version{}, false
	}
	for i, field := range fields {
		part, err := strconv.Atoi(field)
		if err != nil || part < 0 {
			return version{}, false
		}
		parsed[i] = part
	}
	return parsed, true
}

func compare(a, b version) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// extract reads the executable out of a downloaded archive.
func extract(staged string, platform Platform) ([]byte, error) {
	if platform.GOOS == "windows" {
		return extractZip(staged, platform)
	}
	return extractTarGz(staged, platform)
}

func extractTarGz(staged string, platform Platform) ([]byte, error) {
	file, err := os.Open(staged)
	if err != nil {
		return nil, fmt.Errorf("updater: open the archive: %w", err)
	}
	defer func() { _ = file.Close() }()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("updater: read the archive: %w", err)
	}
	defer func() { _ = gzipReader.Close() }()

	reader := tar.NewReader(gzipReader)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("updater: read the archive: %w", err)
		}
		if !header.FileInfo().Mode().IsRegular() || path.Base(header.Name) != platform.executable() {
			continue
		}
		return readBinary(reader)
	}
	return nil, noBinaryError(platform)
}

func extractZip(staged string, platform Platform) ([]byte, error) {
	reader, err := zip.OpenReader(staged)
	if err != nil {
		return nil, fmt.Errorf("updater: read the archive: %w", err)
	}
	defer func() { _ = reader.Close() }()

	for _, file := range reader.File {
		if file.FileInfo().IsDir() || path.Base(file.Name) != platform.executable() {
			continue
		}
		opened, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("updater: read %s: %w", file.Name, err)
		}
		defer func() { _ = opened.Close() }()
		return readBinary(opened)
	}
	return nil, noBinaryError(platform)
}

func readBinary(reader io.Reader) ([]byte, error) {
	binary, err := io.ReadAll(io.LimitReader(reader, maxAssetBytes+1))
	if err != nil {
		return nil, fmt.Errorf("updater: read the binary: %w", err)
	}
	if len(binary) > maxAssetBytes {
		return nil, fmt.Errorf("updater: the binary is larger than the size cap")
	}
	return binary, nil
}

func noBinaryError(platform Platform) error {
	return fmt.Errorf("updater: the archive holds no %s binary", platform.executable())
}

// replace writes data next to target and moves it into place. A file that is
// running cannot be overwritten on Windows, so there the old one steps aside
// first and is removed once the new one owns the path.
func replace(target string, data []byte) error {
	dir := filepath.Dir(target)
	// An interrupted earlier run can leave the binary it stepped aside.
	_ = os.Remove(target + ".old")

	file, err := os.CreateTemp(dir, ".aegis-next-*")
	if err != nil {
		return fmt.Errorf("updater: stage the new binary in %s: %w", dir, err)
	}
	staged := file.Name()
	defer func() { _ = os.Remove(staged) }()

	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("updater: stage the new binary: %w", err)
	}
	if err := file.Chmod(0o755); err != nil {
		_ = file.Close()
		return fmt.Errorf("updater: mark the new binary executable: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("updater: stage the new binary: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("updater: stage the new binary: %w", err)
	}

	if err := os.Rename(staged, target); err == nil {
		return nil
	}
	if err := os.Rename(target, target+".old"); err != nil {
		return fmt.Errorf("updater: replace %s: %w", target, err)
	}
	if err := os.Rename(staged, target); err != nil {
		_ = os.Rename(target+".old", target)
		return fmt.Errorf("updater: replace %s: %w", target, err)
	}
	_ = os.Remove(target + ".old")
	return nil
}

func (p Platform) arch() string {
	if p.GOARCH == "arm" {
		return "armv" + p.GOARM
	}
	return p.GOARCH
}

func (p Platform) extension() string {
	if p.GOOS == "windows" {
		return ".zip"
	}
	return ".tar.gz"
}

func (p Platform) label() string {
	return p.GOOS + "/" + p.arch()
}

func (p Platform) executable() string {
	if p.GOOS == "windows" {
		return executableName + ".exe"
	}
	return executableName
}
