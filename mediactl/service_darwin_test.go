//go:build darwin && cgo

package mediactl

import (
	"runtime/cgo"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/bjarneo/cliamp/internal/playback"
)

func TestDarwinCallbacksUseHandleRouting(t *testing.T) {
	msgsA := make(chan tea.Msg, 1)
	svcA := &Service{send: func(msg tea.Msg) { msgsA <- msg }}
	handleA := cgo.NewHandle(svcA)
	defer handleA.Delete()

	msgsB := make(chan tea.Msg, 1)
	svcB := &Service{send: func(msg tea.Msg) { msgsB <- msg }}
	handleB := cgo.NewHandle(svcB)
	defer handleB.Delete()

	mediaNext(uintptr(handleB))

	select {
	case got := <-msgsB:
		if _, ok := got.(playback.NextMsg); !ok {
			t.Fatalf("goMediaNext() sent %T, want %T", got, playback.NextMsg{})
		}
	default:
		t.Fatal("goMediaNext() did not send a message to the owning service")
	}

	select {
	case got := <-msgsA:
		t.Fatalf("goMediaNext() unexpectedly sent %T to a different service", got)
	default:
	}
}

func TestDarwinSetPositionCallbackConvertsToMicroseconds(t *testing.T) {
	msgs := make(chan tea.Msg, 1)
	svc := &Service{send: func(msg tea.Msg) { msgs <- msg }}
	handle := cgo.NewHandle(svc)
	defer handle.Delete()

	mediaSetPosition(uintptr(handle), 12.25)

	select {
	case got := <-msgs:
		want := playback.SetPositionMsg{Position: 12250 * time.Millisecond}
		if got != want {
			t.Fatalf("goMediaSetPosition() sent %#v, want %#v", got, want)
		}
	default:
		t.Fatal("goMediaSetPosition() did not send a message")
	}
}

func TestDarwinNewRejectsSecondActiveService(t *testing.T) {
	svc, err := New(func(tea.Msg) {})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := New(func(tea.Msg) {}); err == nil {
		t.Fatal("second New() succeeded, want error")
	}

	svc.Close()

	svc2, err := New(func(tea.Msg) {})
	if err != nil {
		t.Fatalf("New() after Close() error = %v", err)
	}
	svc2.Close()
}

func TestDarwinUpdateCoalescesPendingState(t *testing.T) {
	svc := &Service{updates: make(chan updateReq, 1)}

	svc.Update(playback.State{
		Status:   playback.StatusPaused,
		Track:    playback.Track{Title: "first"},
		Position: time.Second,
		Seekable: true,
	})
	svc.Update(playback.State{
		Status:   playback.StatusPlaying,
		Track:    playback.Track{Title: "second", Artist: "artist", ArtURL: "file:///tmp/cover.jpg"},
		Position: 2250 * time.Millisecond,
		Seekable: false,
	})

	select {
	case got := <-svc.updates:
		want := updateReq{
			title:        "second",
			artist:       "artist",
			artURL:       "file:///tmp/cover.jpg",
			durationSecs: 0,
			elapsedSecs:  2.25,
			status:       playback.StatusPlaying,
			canSeek:      false,
		}
		if got != want {
			t.Fatalf("Update() queued %#v, want %#v", got, want)
		}
	default:
		t.Fatal("Update() did not queue state")
	}

	select {
	case got := <-svc.updates:
		t.Fatalf("Update() left stale pending state %#v", got)
	default:
	}
}
func TestIsRemoteArtURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"empty", "", false},
		{"local file", "file:///tmp/cover.jpg", false},
		{"http", "http://example.com/cover.jpg", true},
		{"https", "https://example.com/cover.jpg", true},
		{"uppercase scheme", "HTTPS://example.com/cover.jpg", false},
		{"ftp not remote", "ftp://example.com/cover.jpg", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRemoteArtURL(tt.url); got != tt.want {
				t.Fatalf("isRemoteArtURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}
