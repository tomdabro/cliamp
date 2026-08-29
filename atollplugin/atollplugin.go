// Package atollplugin relays now-playing state to Atoll
// (https://github.com/Ebullioscopic/Atoll), a macOS Dynamic Island app, via
// its AtollPluginManager broker, and applies playback commands (play,
// pause, next, previous, seek) Atoll sends back in response to notch
// controls. Optional and macOS-only: cliamp works exactly the same with or
// without it, and the broker doesn't need to be installed or running for
// cliamp to start.
//
// cliamp is the plugin's socket listener (mirrors ipc.Server, which cliamp
// already runs for its own remote-control protocol); AtollPluginManager
// connects in as a client and exchanges newline-delimited JSON messages. See
// AtollPluginManager's MediaPluginProtocol.swift for the wire format this
// package reads and writes.
package atollplugin

// protocolVersion tracks AtollPluginManager's PluginProtocolVersion.current.
// Bump alongside any wire-shape change on either side.
const protocolVersion = 1

const pluginID = "cliamp"

// nowPlayingMessage is the plugin -> broker line: mirrors
// AtollPluginManager's MediaNowPlayingMessage. The manifest's own
// id/name/supportsSeek/supportsSkip double as the source's registration —
// there's no separate "register" message.
type nowPlayingMessage struct {
	Title         string  `json:"title"`
	Artist        string  `json:"artist,omitempty"`
	Album         string  `json:"album,omitempty"`
	ArtworkBase64 string  `json:"artworkBase64,omitempty"`
	IsPlaying     bool    `json:"isPlaying"`
	ElapsedTime   float64 `json:"elapsedTime"`
	Duration      float64 `json:"duration,omitempty"`
}

// mediaCommandMessage is the broker -> plugin line: mirrors
// AtollPluginManager's MediaCommandMessage.
type mediaCommandMessage struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	SeekTo  *float64 `json:"seekTo"`
}

// manifest mirrors AtollPluginManager's PluginManifest (PluginManifest.swift).
type manifest struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Category        string `json:"category"`
	Transport       string `json:"transport"`
	SocketPath      string `json:"socketPath"`
	ProtocolVersion int    `json:"protocolVersion"`
	SupportsSeek    bool   `json:"supportsSeek"`
	SupportsSkip    bool   `json:"supportsSkip"`
}
