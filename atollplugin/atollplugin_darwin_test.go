//go:build darwin

package atollplugin

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/bjarneo/cliamp/internal/playback"
)

func TestWriteManifestProducesValidPluginJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	socketPath := filepath.Join(home, ".local", "share", "cliamp", "atoll-plugin.sock")
	if err := writeManifest(socketPath); err != nil {
		t.Fatalf("writeManifest: %v", err)
	}

	manifestPath := filepath.Join(home, "Library", "Application Support", "AtollPluginManager", "Plugins", "cliamp", "plugin.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("reading manifest: %v", err)
	}

	var got manifest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}

	want := manifest{
		ID:              "cliamp",
		Name:            "cliamp",
		Category:        "media",
		Transport:       "unixSocket",
		SocketPath:      socketPath,
		ProtocolVersion: protocolVersion,
		SupportsSeek:    true,
		SupportsSkip:    true,
	}
	if got != want {
		t.Errorf("manifest = %+v, want %+v", got, want)
	}
}

// testSocketPath returns a short /tmp path: sockaddr_un.sun_path is capped
// at 104 bytes on Darwin, and t.TempDir() routinely produces paths near
// that limit once a filename is appended.
func testSocketPath(t *testing.T) string {
	t.Helper()
	return filepath.Join("/tmp", "cliamp-atollplugin-test-"+time.Now().Format("150405.000000")+".sock")
}

func TestServiceRelaysNowPlayingOverSocket(t *testing.T) {
	socketPath := testSocketPath(t)
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(socketPath) })

	svc := &Service{listener: listener}
	go svc.acceptLoop()
	t.Cleanup(func() { _ = svc.Close() })

	brokerConn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = brokerConn.Close() })
	reader := bufio.NewReader(brokerConn)

	// Give acceptLoop a moment to register the connection.
	waitFor(t, func() bool {
		svc.mu.Lock()
		defer svc.mu.Unlock()
		return svc.conn != nil
	})

	svc.Update(playback.State{
		Status:   playback.StatusPlaying,
		Track:    playback.Track{Title: "Song Title", Artist: "Artist Name", Duration: 3 * time.Minute},
		Position: 30 * time.Second,
	})

	line := readLine(t, reader)
	var msg nowPlayingMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		t.Fatalf("unmarshal nowPlaying message: %v", err)
	}
	if msg.Title != "Song Title" || msg.Artist != "Artist Name" || !msg.IsPlaying {
		t.Errorf("nowPlaying message = %+v, want playing Song Title/Artist Name", msg)
	}
	if msg.ElapsedTime != 30 || msg.Duration != 180 {
		t.Errorf("nowPlaying elapsedTime/duration = %v/%v, want 30/180", msg.ElapsedTime, msg.Duration)
	}

	svc.Update(playback.State{
		Status: playback.StatusPaused,
		Track:  playback.Track{Title: "Song Title", Artist: "Artist Name"},
	})
	line = readLine(t, reader)
	var paused nowPlayingMessage
	if err := json.Unmarshal(line, &paused); err != nil {
		t.Fatalf("unmarshal paused message: %v", err)
	}
	if paused.IsPlaying {
		t.Errorf("nowPlaying message after pause = %+v, want isPlaying=false", paused)
	}
}

func TestServiceUpdateAndCloseAreNilSafe(t *testing.T) {
	var svc *Service
	svc.Update(playback.State{Status: playback.StatusPlaying, Track: playback.Track{Title: "x"}})
	svc.Seeked(time.Second)
	if err := svc.Close(); err != nil {
		t.Errorf("Close() on nil *Service = %v, want nil", err)
	}
}

// StatusStopped/empty-title Updates must be silently dropped, not sent as a
// nowPlaying snapshot with an empty title (which the broker rejects).
func TestUpdateWithStoppedStatusIsNotSent(t *testing.T) {
	socketPath := testSocketPath(t)
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(socketPath) })

	svc := &Service{listener: listener}
	go svc.acceptLoop()
	t.Cleanup(func() { _ = svc.Close() })

	brokerConn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = brokerConn.Close() })
	reader := bufio.NewReader(brokerConn)
	waitFor(t, func() bool {
		svc.mu.Lock()
		defer svc.mu.Unlock()
		return svc.conn != nil
	})

	svc.Update(playback.State{Status: playback.StatusStopped})

	// Prove nothing arrived for the stopped Update by sending a distinct
	// follow-up message and asserting *that* is the first line the broker
	// sees.
	svc.Update(playback.State{Status: playback.StatusPlaying, Track: playback.Track{Title: "Recovered"}})
	line := readLine(t, reader)
	var msg nowPlayingMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.Title != "Recovered" {
		t.Errorf("first line title = %q, want %q (stopped Update must not have been sent)", msg.Title, "Recovered")
	}
}

func TestReadCommandsDispatchesPlaybackMessages(t *testing.T) {
	socketPath := testSocketPath(t)
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(socketPath) })

	msgs := make(chan tea.Msg, 8)
	svc := &Service{listener: listener, send: func(m tea.Msg) { msgs <- m }}
	go svc.acceptLoop()
	t.Cleanup(func() { _ = svc.Close() })

	brokerConn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = brokerConn.Close() })

	// A new connection sends RefreshMsg first (see
	// TestAcceptRequestsRefreshOnNewConnection) — drain it before asserting
	// on command dispatch below.
	select {
	case got := <-msgs:
		if _, ok := got.(playback.RefreshMsg); !ok {
			t.Fatalf("first dispatched message = %#v, want playback.RefreshMsg{}", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the connect-time RefreshMsg")
	}

	send := func(command string, seekTo *float64) {
		data, err := json.Marshal(mediaCommandMessage{Type: "mediaCommand", Command: command, SeekTo: seekTo})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if _, err := brokerConn.Write(append(data, '\n')); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	cases := []struct {
		command string
		seekTo  *float64
		want    tea.Msg
	}{
		{"play", nil, playback.PlayMsg{}},
		{"pause", nil, playback.PauseMsg{}},
		{"togglePlayPause", nil, playback.PlayPauseMsg{}},
		{"nextTrack", nil, playback.NextMsg{}},
		{"previousTrack", nil, playback.PrevMsg{}},
		{"seek", new(42.5), playback.SetPositionMsg{Position: 42500 * time.Millisecond}},
	}
	for _, tc := range cases {
		send(tc.command, tc.seekTo)
		select {
		case got := <-msgs:
			if got != tc.want {
				t.Errorf("command %q dispatched %#v, want %#v", tc.command, got, tc.want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("command %q: timed out waiting for dispatch", tc.command)
		}
	}
}

// Regression: a broker that connects after the last state change (the
// common case for the interactive TUI, which has no periodic re-push) must
// still learn what's currently playing, not wait for the next unrelated
// user action.
func TestAcceptRequestsRefreshOnNewConnection(t *testing.T) {
	socketPath := testSocketPath(t)
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(socketPath) })

	msgs := make(chan tea.Msg, 8)
	svc := &Service{listener: listener, send: func(m tea.Msg) { msgs <- m }}
	go svc.acceptLoop()
	t.Cleanup(func() { _ = svc.Close() })

	firstConn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	select {
	case got := <-msgs:
		if _, ok := got.(playback.RefreshMsg); !ok {
			t.Fatalf("message on first connect = %#v, want playback.RefreshMsg{}", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for RefreshMsg on first connect")
	}

	// A second connection replacing the first (broker restart / reconnect)
	// must trigger its own refresh, not just the very first one ever.
	_ = firstConn.Close()
	secondConn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = secondConn.Close() })
	select {
	case got := <-msgs:
		if _, ok := got.(playback.RefreshMsg); !ok {
			t.Fatalf("message on reconnect = %#v, want playback.RefreshMsg{}", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for RefreshMsg on reconnect")
	}
}

// End-to-end through Update(), not just artworkCache directly: a cache miss
// must still send the nowPlaying line (without artwork), trigger a
// background fetch, and — once that fetch lands — a RefreshMsg that the
// caller (Model/daemon) is expected to turn into a follow-up Update() that
// this time finds the artwork cached.
func TestUpdateSendsArtworkOnceCachedAfterAsyncFetch(t *testing.T) {
	artDir := t.TempDir()
	artPath := filepath.Join(artDir, "cover.png")
	if err := os.WriteFile(artPath, tinyPNG, 0o644); err != nil {
		t.Fatalf("write test artwork: %v", err)
	}

	socketPath := testSocketPath(t)
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(socketPath) })

	msgs := make(chan tea.Msg, 8)
	svc := &Service{listener: listener, send: func(m tea.Msg) { msgs <- m }, artwork: newArtworkCache()}
	go svc.acceptLoop()
	t.Cleanup(func() { _ = svc.Close() })

	brokerConn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = brokerConn.Close() })
	reader := bufio.NewReader(brokerConn)

	// Drain the connect-time RefreshMsg.
	select {
	case <-msgs:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the connect-time RefreshMsg")
	}

	track := playback.Track{Title: "Song", Artist: "Artist", ArtURL: "file://" + artPath}
	svc.Update(playback.State{Status: playback.StatusPlaying, Track: track})

	line := readLine(t, reader)
	var first nowPlayingMessage
	if err := json.Unmarshal(line, &first); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if first.ArtworkBase64 != "" {
		t.Fatalf("first Update() shipped artwork on a cache miss, want empty (fetch is async)")
	}

	select {
	case got := <-msgs:
		if _, ok := got.(playback.RefreshMsg); !ok {
			t.Fatalf("message after artwork fetch = %#v, want playback.RefreshMsg{}", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the post-fetch RefreshMsg")
	}

	svc.Update(playback.State{Status: playback.StatusPlaying, Track: track})
	line = readLine(t, reader)
	var second nowPlayingMessage
	if err := json.Unmarshal(line, &second); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if second.ArtworkBase64 == "" {
		t.Fatal("second Update() after the fetch completed shipped no artwork, want cached base64 data")
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition never became true")
}

func readLine(t *testing.T, r *bufio.Reader) []byte {
	t.Helper()
	done := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		line, _, err := r.ReadLine()
		if err != nil {
			errCh <- err
			return
		}
		done <- line
	}()
	select {
	case line := <-done:
		return line
	case err := <-errCh:
		t.Fatalf("reading line: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a line")
	}
	return nil
}
