# Music for desktop

A Tauri 2 app for Linux and Windows (macOS works but isn't a focus). It shows
the web app's UI in the system webview: WebKitGTK on Linux, WebView2 on
Windows. Nothing is bundled from a browser, so the app itself is a 5 MB
binary.

- `src-tauri/` is the Rust app: it opens the window, keeps it on the app's
  own pages, and hands every other link (downloads, share links, the privacy
  policy) to the default browser. It also plays the audio (see below).
- The UI is the web app from `src/`, built as static files with
  `bun run build:desktop` (into `.next-desktop/` at the repository root) and
  compiled into the binary.

## Requirements

- [Rust](https://rustup.rs) (stable) and [Bun](https://bun.sh).
- Linux libraries:
  - Arch: `webkit2gtk-4.1 base-devel openssl librsvg xdotool alsa-lib dbus`
  - Debian / Ubuntu: `libwebkit2gtk-4.1-dev libayatana-appindicator3-dev librsvg2-dev libxdo-dev libssl-dev libasound2-dev libdbus-1-dev build-essential file`
  - Fedora: `webkit2gtk4.1-devel openssl-devel libappindicator-gtk3-devel librsvg2-devel libxdo-devel alsa-lib-devel dbus-devel`
    and the `c-development` group
- Windows: the Visual Studio Build Tools with "Desktop development with C++".
  WebView2 is part of Windows 10 and 11; the installer fetches it elsewhere.

## Running and building

```sh
bun install            # at the repository root, for the web UI
cd desktop
bun install
bun run dev            # the web UI with hot reload (port 3010) in the app window
bun run build          # installers in src-tauri/target/release/bundle/
```

In `bun run dev` the UI's API requests go through the Next dev server, which
forwards them to `https://music.cornspies.com` or to the server set in
`MUSIC_DESKTOP_DEV_SERVER` (for example
`MUSIC_DESKTOP_DEV_SERVER=http://localhost:8080 bun run dev`). The sign-in
screen has no server field in this mode.

On Linux the build makes a `.deb`, an `.rpm` and an AppImage; on Windows an
NSIS installer (`.exe`) and an `.msi`. The **Desktop** workflow in GitHub
Actions (run by hand) builds both platforms and attaches the installers to the
run.

## Server

The app asks for the server's address on the sign-in screen and uses
`https://music.cornspies.com` until another one is entered. Plain `http://` is
only accepted without a warning for local network addresses.

The server has to allow the app's origins (`tauri://localhost` on Linux,
`http://tauri.localhost` on Windows) for cross-origin requests; servers from
this version on always do. Older servers answer but refuse the app, and the
sign-in screen says so.

## Audio

The Rust side plays the audio, not the webview: it streams the track from the
server, decodes it (MP3, FLAC, WAV, AIFF, Ogg Vorbis, AAC and ALAC; tracks
the server keeps a compatible copy of, such as Opus, play from that copy),
converts it to the output device's sample rate and plays it with ALSA on Linux
(which reaches PipeWire or PulseAudio) and WASAPI on Windows. Track changes are
gapless, crossfades and loudness normalisation work as in the web player, and
the system media controls (MPRIS on Linux, the media overlay on Windows) show
the track and control playback. The output device is released after 30 seconds
without playback.

The silence-trim preview in the track editor plays through the same engine.

## Closing the window

Closing the window while music plays keeps it playing: the app drops the
window and its web view (most of its memory) and carries on from a tray icon
and the system media controls. The queue, repeat and shuffle, the sleep timer,
play counts and the saved position keep working. Opening the window again
(from the tray, the media controls or by starting the app) picks up the track
that's playing. Closing it while nothing plays quits the app.

On Windows the window is hidden instead, because the media overlay belongs to
it. The same happens during a Listen Together session, which needs the page
running.

## Offline

"Download for offline" saves the track as a file in the app's data folder
(`~/.local/share/com.cornspies.music/offline` on Linux,
`%APPDATA%\com.cornspies.music\offline` on Windows). Downloaded tracks always
play from that file, with or without a connection, and their covers come
from it too.

## Updates

At start and twice a day the app asks its server for a newer build of the
same kind (AppImage, `.deb`, `.rpm` or Windows installer) and offers to
download it. To publish a build, put it in the server's app folder next to
the Android APKs (`MUSIC_APK_DIR`, see `.env.example`) under the name the
build produced, for example `Music_0.2.0_amd64.AppImage`. The version is
`version` in `src-tauri/tauri.conf.json`.

## Links

Shared links (playlists, tracks, Listen Together and sign-up invites) are web
addresses, and Linux and Windows always give those to the browser. So the app
registers its own `com.cornspies.music://` scheme: when one of those pages
opens in a browser on Linux or Windows, it offers **Open in the desktop app**,
and after the first time it opens the app by itself (the same banner turns
that off again). A link to a different server than the one the app uses shows
a notice with a button to open it in the browser.

- The `.deb`, `.rpm`, `.exe` and `.msi` installers register the scheme. The
  AppImage registers itself whenever it starts
  (`~/.local/share/applications/music-desktop-handler.desktop`).
- Only one copy of the app runs: opening a link or starting the app again
  brings the open window to the front.

## Debugging

`bun run tauri build --features devtools` makes a release build that can be
inspected. Start it with `WEBKIT_INSPECTOR_HTTP_SERVER=127.0.0.1:9222` on Linux
and open that address in a browser. In `bun run dev` the inspector is always
available from the context menu.

If the window stays blank on Linux (seen with some NVIDIA drivers), start the
app with `WEBKIT_DISABLE_DMABUF_RENDERER=1`.
