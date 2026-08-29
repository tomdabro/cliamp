//go:build darwin

package atollplugin

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// artworkFetchLimit caps how much of a cover image gets read and shipped as
// base64 over the plugin socket. Typical covers are well under this; a
// malicious or misconfigured server serving something huge must not be
// allowed to balloon memory or the JSON line.
const artworkFetchLimit = 8 * 1024 * 1024 // 8 MiB

// artworkFetchCooldown bounds how often a URL that just failed gets retried,
// mirroring mediactl's own remote-artwork cache (see mediactl/artwork_darwin.go)
// so a broken or slow cover URL doesn't get hammered on every track re-Update.
const artworkFetchCooldown = time.Minute

// artworkCache fetches and base64-encodes cover art off the caller's
// goroutine — local file reads and, more importantly, remote HTTP fetches
// must never block Update(), which runs synchronously on the TUI's main
// update loop. A cache hit is free; a miss returns immediately and the
// result lands asynchronously, after which onReady fires so the caller can
// resend a nowPlaying snapshot that now has artwork.
type artworkCache struct {
	client *http.Client

	mu       sync.Mutex
	data     map[string]string    // url -> base64
	order    []string             // insertion order, for eviction
	inFlight map[string]bool      // url -> fetch currently running
	cooldown map[string]time.Time // url -> retry-not-before, after a failure
}

// artworkCacheLimit bounds how many decoded covers stay resident. Base64
// bloats size ~33%; at a typical 100-300KB cover this comfortably fits in a
// few MB even at the cap.
const artworkCacheLimit = 50

func newArtworkCache() *artworkCache {
	return &artworkCache{
		client:   &http.Client{Timeout: 10 * time.Second},
		data:     make(map[string]string),
		inFlight: make(map[string]bool),
		cooldown: make(map[string]time.Time),
	}
}

// get returns the cached base64 artwork for url, if any.
func (c *artworkCache) get(url string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, ok := c.data[url]
	return data, ok
}

// fetchAsync kicks off a background fetch for url unless one is already
// cached, already in flight, or the URL is under cooldown from a recent
// failure. onReady fires exactly once, only on success, off the caller's
// goroutine.
func (c *artworkCache) fetchAsync(rawURL string, onReady func()) {
	if rawURL == "" {
		return
	}
	c.mu.Lock()
	if _, cached := c.data[rawURL]; cached {
		c.mu.Unlock()
		return
	}
	if c.inFlight[rawURL] {
		c.mu.Unlock()
		return
	}
	if until, onCooldown := c.cooldown[rawURL]; onCooldown && time.Now().Before(until) {
		c.mu.Unlock()
		return
	}
	c.inFlight[rawURL] = true
	c.mu.Unlock()

	go func() {
		defer func() {
			c.mu.Lock()
			delete(c.inFlight, rawURL)
			c.mu.Unlock()
		}()

		encoded, err := c.fetch(rawURL)
		c.mu.Lock()
		if err != nil {
			c.cooldown[rawURL] = time.Now().Add(artworkFetchCooldown)
			c.mu.Unlock()
			return
		}
		delete(c.cooldown, rawURL)
		c.data[rawURL] = encoded
		c.order = append(c.order, rawURL)
		for len(c.order) > artworkCacheLimit {
			oldest := c.order[0]
			c.order = c.order[1:]
			delete(c.data, oldest)
		}
		c.mu.Unlock()

		onReady()
	}()
}

func (c *artworkCache) fetch(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	var raw []byte
	switch parsed.Scheme {
	case "file", "":
		raw, err = os.ReadFile(parsed.Path)
	case "http", "https":
		raw, err = c.fetchHTTP(rawURL)
	default:
		return "", errUnsupportedArtworkScheme
	}
	if err != nil {
		return "", err
	}

	// Sanity-check the bytes actually look like an image before caching and
	// shipping them, mirroring mediactl's own guard against caching a non-
	// image response (e.g. an HTML error page served with a 200).
	if !looksLikeImage(raw) {
		return "", errNotAnImage
	}

	return base64Encode(raw), nil
}

func (c *artworkCache) fetchHTTP(rawURL string) ([]byte, error) {
	resp, err := c.client.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errArtworkFetchFailed
	}
	return io.ReadAll(io.LimitReader(resp.Body, artworkFetchLimit))
}

func looksLikeImage(data []byte) bool {
	return strings.HasPrefix(http.DetectContentType(data), "image/")
}

var (
	errUnsupportedArtworkScheme = artworkError("unsupported artwork URL scheme")
	errNotAnImage               = artworkError("response is not an image")
	errArtworkFetchFailed       = artworkError("artwork fetch failed")
)

type artworkError string

func (e artworkError) Error() string { return string(e) }

func base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}
