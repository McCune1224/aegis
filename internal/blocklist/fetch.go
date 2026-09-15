package blocklist

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// maxListBytes caps how much of a response is read, so a broken or hostile
// source cannot exhaust memory.
const maxListBytes = 32 << 20

// maxRedirects is how many hops a fetch follows before it gives up.
const maxRedirects = 5

// ErrTooLarge reports a response past the size cap.
var ErrTooLarge = errors.New("blocklist: response is larger than the size cap")

// FetchResult is one fetch.
type FetchResult struct {
	Body        []byte
	ETag        string
	NotModified bool
}

// Fetcher downloads list bodies with a bounded timeout, a size cap, and a
// redirect limit.
type Fetcher struct {
	client *http.Client
}

// NewFetcher returns a Fetcher whose timeout covers the whole request,
// redirects included.
func NewFetcher(timeout time.Duration) *Fetcher {
	return &Fetcher{client: &http.Client{
		Timeout: timeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("blocklist: too many redirects")
			}
			if request.URL.Scheme != "http" && request.URL.Scheme != "https" {
				return fmt.Errorf("blocklist: redirect to unsupported scheme %q", request.URL.Scheme)
			}
			return nil
		},
	}}
}

// Fetch downloads url. An empty etag fetches the body. A matching etag makes the
// request conditional, and a 304 comes back as NotModified with no body.
func (f *Fetcher) Fetch(ctx context.Context, url, etag string) (FetchResult, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return FetchResult{}, fmt.Errorf("blocklist: %w", err)
	}
	if request.URL.Scheme != "http" && request.URL.Scheme != "https" {
		return FetchResult{}, fmt.Errorf("blocklist: unsupported scheme %q", request.URL.Scheme)
	}
	if etag != "" {
		request.Header.Set("If-None-Match", etag)
	}

	response, err := f.client.Do(request)
	if err != nil {
		return FetchResult{}, fmt.Errorf("blocklist: fetch %s: %w", url, err)
	}
	defer func() { _ = response.Body.Close() }()

	switch response.StatusCode {
	case http.StatusNotModified:
		return FetchResult{ETag: etag, NotModified: true}, nil
	case http.StatusOK:
	default:
		return FetchResult{}, fmt.Errorf("blocklist: fetch %s: status %s", url, response.Status)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxListBytes+1))
	if err != nil {
		return FetchResult{}, fmt.Errorf("blocklist: read %s: %w", url, err)
	}
	if len(body) > maxListBytes {
		return FetchResult{}, ErrTooLarge
	}
	return FetchResult{Body: body, ETag: response.Header.Get("ETag")}, nil
}
