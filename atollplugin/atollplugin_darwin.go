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

	tea "charm.land/bubbletea/v2"

	"github.com/bjarneo/cliamp/applog"
	"github.com/bjarneo/cliamp/internal/appdir"
	"github.com/bjarneo/cliamp/internal/playback"
)

// Service implements playback.Notifier by relaying state to whichever
// AtollPluginManager broker connects to its Unix socket, and applies
// playback commands the broker relays back from Atoll's notch controls.
// There is no outbound connection here: cliamp listens, matching how
// ipc.Server already works for cliamp's own remote-control protocol.
type Service struct {
	listener net.Listener
	send     func(tea.Msg)
	artwork  *artworkCache

	mu   sync.Mutex
	conn net.Conn
}

// New starts listening on cliamp's Atoll plugin socket and writes a
// plugin.json manifest where AtollPluginManager's passive discovery looks
// for it. send delivers playback commands relayed from Atoll (play, pause,
// next, previous, seek) the same way mediactl delivers OS media-key events.
// Returns (nil, nil) if either step fails — Atoll integration is optional
// and must never keep cliamp from starting.
func New(send func(tea.Msg)) (*Service, error) {
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

	s := &Service{listener: listener, send: send, artwork: newArtworkCache()}
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
		s.mu.Unlock()
		// A newly-connected broker registers this source itself (from the
		// manifest, on its own connect), but has no way to learn what's
		// already playing until the next state change — ask the owner to
		// re-push current state now rather than leaving Atoll blank until
		// the user's next play/pause/skip.
		if s.send != nil {
			s.send(playback.RefreshMsg{})
		}
		go s.readCommands(conn)
	}
}

// readCommands parses mediaCommand lines the broker relays from Atoll and
// dispatches them as playback messages, same shape as mediactl's OS-driven
// media-key handlers. Also notices the connection closing so a later Update
// knows to wait for the next Accept.
func (s *Service) readCommands(conn net.Conn) {
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var msg mediaCommandMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			applog.Warn("atollplugin: malformed command from broker: %v", err)
			continue
		}
		s.dispatch(msg)
	}
	s.mu.Lock()
	if s.conn == conn {
		s.conn = nil
	}
	s.mu.Unlock()
}

func (s *Service) dispatch(msg mediaCommandMessage) {
	if s.send == nil {
		return
	}
	switch msg.Command {
	case "play":
		s.send(playback.PlayMsg{})
	case "pause":
		s.send(playback.PauseMsg{})
	case "togglePlayPause":
		s.send(playback.PlayPauseMsg{})
	case "nextTrack":
		s.send(playback.NextMsg{})
	case "previousTrack":
		s.send(playback.PrevMsg{})
	case "seek":
		if msg.SeekTo != nil {
			s.send(playback.SetPositionMsg{Position: time.Duration(*msg.SeekTo * float64(time.Second))})
		}
	default:
		applog.Warn("atollplugin: unknown command from broker: %q", msg.Command)
	}
}

func (s *Service) Update(state playback.State) {
	if s == nil {
		return
	}
	// StatusStopped means "nothing loaded" (see daemon.snapshotState),
	// which media protocol v1 has no explicit way to represent — there's no
	// dismiss/clear message, only nowPlaying snapshots. Simplest correct
	// behavior available: just stop sending; Atoll keeps showing the last
	// known state, matching how most OS Now Playing widgets behave once a
	// player exits without another app claiming Now Playing.
	if state.Status == playback.StatusStopped || state.Track.Title == "" {
		return
	}
	var duration float64
	if state.Track.Duration > 0 {
		duration = state.Track.Duration.Seconds()
	}

	// A cache hit ships immediately. A miss sends this update without
	// artwork and kicks off a background fetch (local file read or remote
	// HTTP GET, either of which would otherwise block this call, which runs
	// synchronously on the TUI's update loop); RefreshMsg re-triggers this
	// same path once the fetch lands, and the retry is then a cache hit.
	var artworkBase64 string
	if state.Track.ArtURL != "" {
		if cached, ok := s.artwork.get(state.Track.ArtURL); ok {
			artworkBase64 = cached
		} else if s.send != nil {
			send := s.send
			s.artwork.fetchAsync(state.Track.ArtURL, func() { send(playback.RefreshMsg{}) })
		}
	}

	s.writeToBroker(nowPlayingMessage{
		Title:         state.Track.Title,
		Artist:        state.Track.Artist,
		Album:         state.Track.Album,
		ArtworkBase64: artworkBase64,
		IsPlaying:     state.Status == playback.StatusPlaying,
		ElapsedTime:   state.Position.Seconds(),
		Duration:      duration,
	})
}

func (s *Service) Seeked(time.Duration) {}

func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	return s.listener.Close()
}

// writeToBroker writes v to whichever broker is currently connected; a no-op if
// none is (passive — Atoll integration is optional).
func (s *Service) writeToBroker(v any) bool {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return false
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
		Category:        "media",
		Transport:       "unixSocket",
		SocketPath:      socketPath, // absolute: cliamp's socket lives outside the plugin folder
		ProtocolVersion: protocolVersion,
		SupportsSeek:    true,
		SupportsSkip:    true,
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644)
}
