# Music for desktop

A Tauri 2 app for Linux and Windows. It shows the web app's UI (`src/`) in the
system webview and plays the audio itself, in Rust (`src-tauri/`). Everything
below is described in full in [docs/desktop.md](../docs/desktop.md).

## Build and run

You need [Rust](https://rustup.rs), [Bun](https://bun.sh) and the system
libraries listed under [Requirements](../docs/desktop.md#requirements).

```sh
bun install            # at the repository root, for the web UI
cd desktop
bun install
bun run dev            # the UI with hot reload in the app window
bun run build          # installers in src-tauri/target/release/bundle/
```

- The build makes a `.deb`, an `.rpm` and an AppImage on Linux, and an
  installer (`.exe`) on Windows. Pushing a tag builds them all in the
  **Release** workflow.
- `bun run dev` uses the backend on `http://localhost:3001`, or the server in
  `MUSIC_DESKTOP_DEV_SERVER`.
- Set `NEXT_PUBLIC_MUSIC_DEFAULT_SERVER` when building to fill in your server
  on the sign-in screen.

## What to know

- **Audio** is decoded and played in Rust: gapless, with crossfade, loudness
  normalisation and the system media controls.
- **Closing the window** quits the app. With **Minimize to tray when closed**
  on (in Settings, off by default), music keeps playing from the tray.
- **Downloads** go to `~/.local/share/com.cornspies.music/offline`
  (`%APPDATA%\com.cornspies.music\offline` on Windows) or the folder chosen in
  Settings. Signing out removes them.
- **Settings** are at the bottom of the sidebar and stored in `settings.json`
  in the app's config folder.
- **Shared links** open in the app through the `cornspies-music://` scheme.
  Only one copy of the app runs.
- **Updates:** the app asks its server for a newer build. Built with an update
  key, an AppImage or Windows install updates itself; otherwise it offers the
  download. See [Updates](../docs/desktop.md#updates).
- **After an update** the app shows that version's key points once. They are
  in `src/lib/whats-new.ts`.

## Debugging

`bun run tauri build --features devtools` makes a build that can be inspected.
If the window stays blank on Linux, start the app with
`WEBKIT_DISABLE_DMABUF_RENDERER=1`. More in
[Debugging](../docs/desktop.md#debugging).
