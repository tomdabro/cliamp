package spotify

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

// Regression: /v1/playlists/{id}/items used a `fields` filter that requested
// only album(name,release_date), silently dropping album.images (and the
// episode-only images/show.images) from Spotify's response — every track
// loaded from a real playlist got Album but never AlbumArtURL, regardless of
// whether the track actually has cover art on Spotify's side.
func TestTracksRequestsAlbumImagesAndPopulatesArtURL(t *testing.T) {
	var gotFields string
	originalTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v1/playlists/pl1/items" {
			return nil, fmt.Errorf("unexpected Spotify API path %q", req.URL.Path)
		}
		gotFields = req.URL.Query().Get("fields")
		body := `{
			"items": [{
				"item": {
					"id": "t0",
					"name": "Elephant",
					"type": "track",
					"uri": "spotify:track:t0",
					"artists": [{"name": "Tame Impala"}],
					"album": {
						"name": "Lonerism",
						"release_date": "2012-10-05",
						"images": [
							{"url": "https://i.scdn.co/image/large", "width": 640, "height": 640},
							{"url": "https://i.scdn.co/image/small", "width": 64, "height": 64}
						]
					}
				}
			}],
			"total": 1
		}`
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
	got, err := New(sess, "client", 320).Tracks("pl1")
	if err != nil {
		t.Fatalf("Tracks() error = %v", err)
	}

	fields, err := url.QueryUnescape(gotFields)
	if err != nil {
		t.Fatalf("unescape fields param: %v", err)
	}
	if !strings.Contains(fields, "album(name,release_date,images)") {
		t.Errorf("fields param = %q, want it to request album images", fields)
	}

	if len(got) != 1 {
		t.Fatalf("got %d tracks, want 1", len(got))
	}
	if got[0].AlbumArtURL != "https://i.scdn.co/image/large" {
		t.Errorf("AlbumArtURL = %q, want the >=160px image", got[0].AlbumArtURL)
	}
}
