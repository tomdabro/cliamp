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
		Category:        "liveActivity",
		Transport:       "unixSocket",
		SocketPath:      socketPath,
		ProtocolVersion: protocolVersion,
	}
	if got != want {
		t.Errorf("manifest = %+v, want %+v", got, want)
	}
}

func TestMessageTypeForTogglesPresentVsUpdate(t *testing.T) {
	if got := messageTypeFor(false); got != "presentActivity" {
		t.Errorf("messageTypeFor(false) = %q, want presentActivity", got)
	}
	if got := messageTypeFor(true); got != "updateActivity" {
		t.Errorf("messageTypeFor(true) = %q, want updateActivity", got)
	}
}

// testSocketPath returns a short /tmp path: sockaddr_un.sun_path is capped
// at 104 bytes on Darwin, and t.TempDir() routinely produces paths near
// that limit once a filename is appended.
func testSocketPath(t *testing.T) string {
	t.Helper()
	return filepath.Join("/tmp", "cliamp-atollplugin-test-"+time.Now().Format("150405.000000")+".sock")
}

func TestServiceRelaysPresentUpdateDismissOverSocket(t *testing.T) {
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
		Status: playback.StatusPlaying,
		Track:  playback.Track{Title: "Song Title", Artist: "Artist Name"},
	})

	line := readLine(t, reader)
	var present presentOrUpdateMessage
	if err := json.Unmarshal(line, &present); err != nil {
		t.Fatalf("unmarshal present message: %v", err)
	}
	if present.Type != "presentActivity" || present.ID != activityID || present.Title != "Song Title" || present.Subtitle != "Artist Name" {
		t.Errorf("present message = %+v, want presentActivity/%s with title+artist", present, activityID)
	}

	svc.Update(playback.State{
		Status: playback.StatusPlaying,
		Track:  playback.Track{Title: "New Title", Artist: "Artist Name"},
	})
	line = readLine(t, reader)
	var updated presentOrUpdateMessage
	if err := json.Unmarshal(line, &updated); err != nil {
		t.Fatalf("unmarshal update message: %v", err)
	}
	if updated.Type != "updateActivity" || updated.Title != "New Title" {
		t.Errorf("update message = %+v, want updateActivity with new title", updated)
	}

	svc.Update(playback.State{Status: playback.StatusStopped})
	line = readLine(t, reader)
	var dismissed dismissMessage
	if err := json.Unmarshal(line, &dismissed); err != nil {
		t.Fatalf("unmarshal dismiss message: %v", err)
	}
	if dismissed.Type != "dismissActivity" || dismissed.ID != activityID {
		t.Errorf("dismiss message = %+v, want dismissActivity/%s", dismissed, activityID)
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

// Regression: an Update() dropped because no broker was connected yet must
// not flip presented to true — otherwise the *next* Update (once a broker
// finally connects) sends updateActivity for an id Atoll never received a
// presentActivity for, and Atoll rejects it.
func TestUpdateBeforeBrokerConnectsDoesNotMarkPresented(t *testing.T) {
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

	// No broker connected: this must be silently dropped, not counted as presented.
	svc.Update(playback.State{Status: playback.StatusPlaying, Track: playback.Track{Title: "Song"}})
	if svc.hasPresented() {
		t.Fatal("hasPresented() = true after an Update with no broker connected, want false")
	}

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

	svc.Update(playback.State{Status: playback.StatusPlaying, Track: playback.Track{Title: "Song"}})
	line := readLine(t, reader)
	var msg presentOrUpdateMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.Type != "presentActivity" {
		t.Errorf("message type = %q, want presentActivity (broker never saw the first, dropped Update)", msg.Type)
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
