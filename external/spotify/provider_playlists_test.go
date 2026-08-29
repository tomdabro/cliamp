package spotify

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestPlaylistsIncludesFollowedPlaylists(t *testing.T) {
	originalTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.URL.Path {
		case "/v1/me":
			body = `{"id":"me"}`
		case "/v1/me/tracks":
			body = `{"total":1}`
		case "/v1/me/playlists":
			body = `{"items":[` +
				`{"id":"owned","name":"Owned","snapshot_id":"one","owner":{"id":"me"},"items":{"total":2}},` +
				`{"id":"collab","name":"Collab","snapshot_id":"two","owner":{"id":"other"},"collaborative":true,"items":{"total":1}},` +
				`{"id":"followed","name":"Followed","snapshot_id":"three","owner":{"id":"other"},"items":{"total":3}}` +
				`],"total":3}`
		default:
			return nil, fmt.Errorf("unexpected Spotify API path %q", req.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	sess := &Session{tokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "token"})}
	got, err := New(sess, "client", 320).Playlists()
	if err != nil {
		t.Fatal(err)
	}

	// Since Spotify's February 2026 Web API changes, GET
	// /v1/playlists/{id}/items 403s for a playlist the user neither owns
	// nor collaborates on -- a plain followed playlist is unaffected here
	// (this is a different endpoint) but gets its own section so the user
	// sees upfront that its tracks can never actually be opened.
	want := []struct {
		id      string
		section string
	}{
		{id: "YOUR MUSIC", section: "Library"},
		{id: "owned", section: "Your playlists"},
		{id: "collab", section: "Followed playlists"},
		{id: "followed", section: "Followed playlists (not playable)"},
	}
	if len(got) != len(want) {
		t.Fatalf("Playlists() returned %d playlists, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].ID != want[i].id || got[i].Section != want[i].section {
			t.Errorf("playlist %d = (%q, %q), want (%q, %q)", i, got[i].ID, got[i].Section, want[i].id, want[i].section)
		}
	}
}

func TestRefreshInvalidatesPlaylistListCache(t *testing.T) {
	originalTransport := http.DefaultTransport
	calls := 0
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.URL.Path {
		case "/v1/me":
			body = `{"id":"me"}`
		case "/v1/me/tracks":
			body = `{"total":0}`
		case "/v1/me/playlists":
			calls++
			body = fmt.Sprintf(`{"items":[{"id":"p%d","name":"P%d","snapshot_id":"s","owner":{"id":"me"},"items":{"total":0}}],"total":1}`, calls, calls)
		default:
			return nil, fmt.Errorf("unexpected Spotify API path %q", req.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	sess := &Session{tokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "token"})}
	p := New(sess, "client", 320)

	first, err := p.Playlists()
	if err != nil {
		t.Fatalf("Playlists() error: %v", err)
	}
	if len(first) != 2 || first[1].ID != "p1" {
		t.Fatalf("first Playlists() = %+v, want a single p1 playlist", first)
	}

	// Within the cache TTL, a second call must not hit the API again -- a
	// newly followed/pinned playlist would silently stay invisible.
	cached, err := p.Playlists()
	if err != nil {
		t.Fatalf("cached Playlists() error: %v", err)
	}
	if calls != 1 || cached[1].ID != "p1" {
		t.Fatalf("expected the cache to serve the second call: calls=%d, got=%+v", calls, cached)
	}

	// Refresh() must invalidate that cache -- this is what the "r" key
	// relies on (playlist.Refresher) to actually pick up changes made in
	// Spotify while cliamp was already running.
	p.Refresh()
	refreshed, err := p.Playlists()
	if err != nil {
		t.Fatalf("Playlists() after Refresh error: %v", err)
	}
	if calls != 2 || refreshed[1].ID != "p2" {
		t.Fatalf("expected Refresh() to force a re-fetch: calls=%d, got=%+v", calls, refreshed)
	}
}
