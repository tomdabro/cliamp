//go:build darwin

package atollplugin

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bjarneo/cliamp/applog"
	"github.com/bjarneo/cliamp/internal/appdir"
	"github.com/bjarneo/cliamp/internal/playback"
)

// Service implements playback.Notifier by relaying state to whichever
// AtollPluginManager broker connects to its Unix socket. There is no
// outbound connection here: cliamp listens, matching how ipc.Server already
// works for cliamp's own remote-control protocol.
type Service struct {
	listener net.Listener

	mu        sync.Mutex
	conn      net.Conn
	presented bool
}

// New starts listening on cliamp's Atoll plugin socket and writes a
// plugin.json manifest where AtollPluginManager's passive discovery looks
// for it. Returns (nil, nil) if either step fails — Atoll integration is
// optional and must never keep cliamp from starting.
func New() (*Service, error) {
	socketPath, err := socketPath()
	if err != nil {
		applog.Warn("atollplugin: resolving socket path: %v", err)
		return nil, nil
	}
	_ = os.Remove(socketPath) // stale socket left by an unclean shutdown

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		applog.Warn("atollplugin: listen on %s: %v", socketPath, err)
		return nil, nil
	}

	if err := writeManifest(socketPath); err != nil {
		applog.Warn("atollplugin: writing plugin.json: %v", err)
		// Not fatal: the socket still works if the manifest is added by hand.
	}

	s := &Service{listener: listener}
	go s.acceptLoop()
	return s, nil
}

func (s *Service) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return // listener closed
		}
		s.mu.Lock()
		if s.conn != nil {
			_ = s.conn.Close()
		}
		s.conn = conn
		// A newly-connected broker has no memory of anything from a previous
		// connection's lifetime (its own reconnect, or cliamp itself having
		// restarted) — the next Update must start with presentActivity, not
		// an updateActivity/dismissActivity for an id it never presented.
		s.presented = false
		s.mu.Unlock()
		go s.readAcks(conn)
	}
}

// readAcks drains the broker's ack/error responses. cliamp doesn't act on
// them; draining just keeps the read side from backing up and notices the
// connection closing so a later Update knows to reconnect on the next Accept.
func (s *Service) readAcks(conn net.Conn) {
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
	}
	s.mu.Lock()
	if s.conn == conn {
		s.conn = nil
	}
	s.mu.Unlock()
}

func (s *Service) Update(state playback.State) {
	if s == nil {
		return
	}
	if state.Status == playback.StatusStopped || state.Track.Title == "" {
		s.dismiss()
		return
	}
	sent := s.send(presentOrUpdateMessage{
		Type:     messageTypeFor(s.hasPresented()),
		ID:       activityID,
		Title:    state.Track.Title,
		Subtitle: state.Track.Artist,
		Icon:     "music.note",
		Priority: "normal",
	})
	// Only mark presented if this actually reached a connected broker —
	// dropping it silently (no broker connected yet) must not make the
	// *next* Update send updateActivity for something Atoll never got.
	if sent {
		s.markPresented(true)
	}
}

func (s *Service) Seeked(time.Duration) {}

func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	s.dismiss()
	return s.listener.Close()
}

func (s *Service) dismiss() {
	if !s.hasPresented() {
		return
	}
	if s.send(dismissMessage{Type: "dismissActivity", ID: activityID}) {
		s.markPresented(false)
	}
}

func (s *Service) hasPresented() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.presented
}

func (s *Service) markPresented(v bool) {
	s.mu.Lock()
	s.presented = v
	s.mu.Unlock()
}

func messageTypeFor(alreadyPresented bool) string {
	if alreadyPresented {
		return "updateActivity"
	}
	return "presentActivity"
}

// send returns whether a broker was actually connected to receive v.
func (s *Service) send(v any) bool {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return false // no broker connected right now; passive, just drop
	}
	data, err := json.Marshal(v)
	if err != nil {
		return false
	}
	data = append(data, '\n')
	_, err = conn.Write(data)
	return err == nil
}

// socketPath lives under cliamp's data dir (~/.local/share/cliamp), not its
// config dir: it's runtime state, same category as the existing IPC socket.
// Kept short — sockaddr_un.sun_path is capped at 104 bytes on Darwin.
func socketPath() (string, error) {
	dir, err := appdir.DataDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "atoll-plugin.sock"), nil
}

func writeManifest(socketPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	pluginDir := filepath.Join(home, "Library", "Application Support", "AtollPluginManager", "Plugins", pluginID)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		return err
	}

	m := manifest{
		ID:              pluginID,
		Name:            "cliamp",
		Category:        "liveActivity",
		Transport:       "unixSocket",
		SocketPath:      socketPath, // absolute: cliamp's socket lives outside the plugin folder
		ProtocolVersion: protocolVersion,
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644)
}
