# Atoll Integration

Optional, macOS-only. If you run
[Atoll](https://github.com/Ebullioscopic/Atoll) (a Dynamic Island app) and
[AtollPluginManager](https://github.com/tomdabro/AtollPluginManager) (its
plugin broker), cliamp registers as a Now Playing media source: Atoll's
notch shows cliamp's current track (title, artist, elapsed/duration) the
same way it shows Music.app or Spotify, and play/pause/next/previous/seek
from Atoll's controls are relayed back into cliamp. Nothing about this
requires either app to be installed or running — cliamp works exactly the
same with or without them.

## What it does

Whenever cliamp's playback state changes (track, artist, play/pause,
position), it sends a Now Playing snapshot to whichever AtollPluginManager
broker is connected. Commands the user issues from Atoll's notch (play,
pause, toggle, next, previous, seek) come back down the same socket and are
applied the same way OS media keys are (see `mediactl/`). This runs in both
the interactive TUI and `--daemon` (headless) modes.

## Setup

1. Install and run [Atoll](https://github.com/Ebullioscopic/Atoll). In
   Atoll's media source picker, select "Third-Party Extension" — once
   cliamp is running and connected through AtollPluginManager, it appears
   there automatically.
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
a client. The manifest's own `category: "media"`, `supportsSeek`, and
`supportsSkip` double as the source's registration with Atoll — there's no
separate register message, just Now Playing snapshots going out and
commands coming back. See `atollplugin/` for the implementation and
[AtollPluginManager's `PROTOCOL.md`](https://github.com/tomdabro/AtollPluginManager/blob/main/PROTOCOL.md)
for the wire format.

## Troubleshooting

- Nothing appears in Atoll: confirm AtollPluginManager is running and shows
  `cliamp` in its plugin list, tagged "Media Source", with a green status
  dot.
- Atoll's media source picker doesn't offer cliamp: select "Third-Party
  Extension" as the controller type first — Atoll only shows a name for it
  once a source has actually registered.
- The socket doesn't exist: check cliamp's log
  (`~/.config/cliamp/cliamp.log`, or `$CLIAMP_CONFIG_DIR/cliamp.log` if set)
  for `atollplugin:` warnings — a failure here never stops cliamp from
  starting, it just means this integration is inactive for that session.
