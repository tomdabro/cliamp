# Atoll Integration

Optional, macOS-only. If you run
[Atoll](https://github.com/Ebullioscopic/Atoll) (a Dynamic Island app) and
[AtollPluginManager](https://github.com/tomdabro/AtollPluginManager) (its
plugin broker), cliamp's now-playing state shows up as a live activity in
the notch. Nothing about this requires either app to be installed or
running — cliamp works exactly the same with or without them.

## What it does

Whenever cliamp's playback state changes (track, artist, play/pause/stop),
it sends a live activity update to whichever AtollPluginManager broker is
connected. This runs in both the interactive TUI and `--daemon` (headless)
modes.

## Setup

1. Install and run [Atoll](https://github.com/Ebullioscopic/Atoll).
2. Install and run
   [AtollPluginManager](https://github.com/tomdabro/AtollPluginManager).
3. Start cliamp. On startup it:
   - listens on `~/.local/share/cliamp/atoll-plugin.sock`, and
   - writes a manifest to
     `~/Library/Application Support/AtollPluginManager/Plugins/cliamp/plugin.json`
     so AtollPluginManager finds it automatically.

No configuration on cliamp's side — there's nothing to enable, and nothing
breaks if the broker isn't running (messages are simply dropped until it
connects).

## How it works

cliamp is the socket listener, mirroring how `ipc/server.go` already works
for cliamp's own remote-control protocol; AtollPluginManager connects in as
a client. See `atollplugin/` for the implementation and
[AtollPluginManager's `PROTOCOL.md`](https://github.com/tomdabro/AtollPluginManager/blob/main/PROTOCOL.md)
for the wire format.

## Troubleshooting

- Nothing appears in Atoll: confirm AtollPluginManager is running and shows
  `cliamp` in its plugin list with a green status dot.
- The socket doesn't exist: check cliamp's log
  (`~/.config/cliamp/cliamp.log`, or `$CLIAMP_CONFIG_DIR/cliamp.log` if set)
  for `atollplugin:` warnings — a failure here never stops cliamp from
  starting, it just means this integration is inactive for that session.
