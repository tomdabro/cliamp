//go:build darwin

package atollplugin

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// a minimal valid 1x1 PNG, enough for http.DetectContentType to recognize as image/png.
var tinyPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
	0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
	0x42, 0x60, 0x82,
}

func waitForArtwork(t *testing.T, c *artworkCache, url string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if data, ok := c.get(url); ok {
			return data
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("artwork for %q was never cached within %s", url, timeout)
	return ""
}

func TestArtworkCacheFetchesAndCachesLocalFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cover.png")
	if err := os.WriteFile(path, tinyPNG, 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	c := newArtworkCache()
	ready := make(chan struct{}, 1)
	c.fetchAsync("file://"+path, func() { ready <- struct{}{} })

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("onReady never fired")
	}

	data, ok := c.get("file://" + path)
	if !ok || data == "" {
		t.Fatalf("get() = %q, %v; want cached base64 data", data, ok)
	}
}

func TestArtworkCacheFetchesRemoteHTTP(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(tinyPNG)
	}))
	defer srv.Close()

	c := newArtworkCache()
	ready := make(chan struct{}, 1)
	c.fetchAsync(srv.URL, func() { ready <- struct{}{} })

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("onReady never fired")
	}

	if _, ok := c.get(srv.URL); !ok {
		t.Fatal("expected remote artwork to be cached after a successful fetch")
	}

	// A second fetch for the same URL must be a cache hit, not another request.
	c.fetchAsync(srv.URL, func() {})
	time.Sleep(100 * time.Millisecond)
	if n := hits.Load(); n != 1 {
		t.Fatalf("server hit %d times, want 1 (cache should dedupe)", n)
	}
}

func TestArtworkCacheRejectsNonImageAndCoolsDown(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>not a cover</html>"))
	}))
	defer srv.Close()

	c := newArtworkCache()
	fetched := make(chan struct{}, 1)
	c.fetchAsync(srv.URL, func() { fetched <- struct{}{} })

	select {
	case <-fetched:
		t.Fatal("onReady must not fire for a non-image response")
	case <-time.After(300 * time.Millisecond):
	}

	if _, ok := c.get(srv.URL); ok {
		t.Fatal("a non-image response must not enter the cache")
	}

	// Cooldown must prevent a second fetch attempt right away.
	c.fetchAsync(srv.URL, func() {})
	time.Sleep(100 * time.Millisecond)
	if n := hits.Load(); n != 1 {
		t.Fatalf("server hit %d times, want 1 (failed fetch should cool down)", n)
	}
}

func TestArtworkCacheGetMissReturnsFalse(t *testing.T) {
	c := newArtworkCache()
	if _, ok := c.get("http://example.com/nope.png"); ok {
		t.Fatal("get() on an empty cache = true, want false")
	}
}

func TestArtworkCacheFetchAsyncIgnoresEmptyURL(t *testing.T) {
	c := newArtworkCache()
	called := false
	c.fetchAsync("", func() { called = true })
	time.Sleep(50 * time.Millisecond)
	if called {
		t.Fatal("fetchAsync must not fetch or call onReady for an empty URL")
	}
}
