//go:build darwin && cgo

package mediactl

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// 1x1 red PNG.
const testPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func TestRemoteArtworkFetchCachesDecodedImage(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(mustDecodePNG(t))
	}))
	defer srv.Close()

	before := remoteArtworkCachedCount()
	scheduleRemoteArtwork(srv.URL)

	deadline := time.Now().Add(5 * time.Second)
	for remoteArtworkCachedCount() <= before && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if remoteArtworkCachedCount() <= before {
		t.Fatal("remote artwork was not cached after fetch")
	}

	// A second schedule must not fetch again: the Go byte cache and the
	// decoded-image cache both dedupe.
	scheduleRemoteArtwork(srv.URL)
	time.Sleep(100 * time.Millisecond)
	if n := hits.Load(); n != 1 {
		t.Fatalf("artwork fetched %d times, want 1", n)
	}
}

func TestRemoteArtworkNonImageIsNotCachedAndRetriesCooldown(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("not an image"))
	}))
	defer srv.Close()

	before := remoteArtworkCachedCount()
	scheduleRemoteArtwork(srv.URL)

	deadline := time.Now().Add(2 * time.Second)
	for hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hits.Load() == 0 {
		t.Fatal("artwork fetch never started")
	}

	// The failed URL is under a one-minute cooldown, so repeated scheduling
	// must not hammer the server.
	scheduleRemoteArtwork(srv.URL)
	scheduleRemoteArtwork(srv.URL)
	time.Sleep(100 * time.Millisecond)
	if n := hits.Load(); n != 1 {
		t.Fatalf("failed artwork fetched %d times, want 1 (cooldown)", n)
	}
	if remoteArtworkCachedCount() != before {
		t.Fatal("non-image response must not enter the decoded cache")
	}
}

func mustDecodePNG(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(testPNG)
	if err != nil {
		t.Fatalf("decode test png: %v", err)
	}
	return data
}
