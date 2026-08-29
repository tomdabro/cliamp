// Package atollplugin relays now-playing state to Atoll
// (https://github.com/Ebullioscopic/Atoll), a macOS Dynamic Island app, via
// its AtollPluginManager broker. Optional and macOS-only: cliamp works
// exactly the same with or without it, and the broker doesn't need to be
// installed or running for cliamp to start.
//
// cliamp is the plugin's socket listener (mirrors ipc.Server, which cliamp
// already runs for its own remote-control protocol); AtollPluginManager
// connects in as a client and reads newline-delimited JSON messages. See
// AtollPluginManager's PluginProtocol.swift for the wire format this
// package writes.
package atollplugin

// protocolVersion tracks AtollPluginManager's PluginProtocolVersion.current.
// Bump alongside any wire-shape change on either side.
const protocolVersion = 1

const activityID = "now-playing"

const pluginID = "cliamp"

type presentOrUpdateMessage struct {
	Type     string `json:"type"`
	ID       string `json:"id"`
	Title    string `json:"title,omitempty"`
	Subtitle string `json:"subtitle,omitempty"`
	Icon     string `json:"icon,omitempty"`
	Priority string `json:"priority,omitempty"`
}

type dismissMessage struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// manifest mirrors AtollPluginManager's PluginManifest (PluginManifest.swift).
type manifest struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Category        string `json:"category"`
	Transport       string `json:"transport"`
	SocketPath      string `json:"socketPath"`
	ProtocolVersion int    `json:"protocolVersion"`
}
