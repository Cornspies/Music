# Music

> **Disclaimer:** this project was developed with the help of AI.

> The iOS app is not on the App Store yet, but will be soon.

A self-hosted music server with apps for the web, Windows, Linux, Android and
iOS. You run one server, upload your own music, and listen from anywhere.

| Part | Folder | Built with |
|---|---|---|
| Server | [backend/music-server](backend/music-server) | Go, SQLite |
| Web app | [src](src) | Next.js, React, Tailwind |
| Desktop app | [desktop](desktop) | Tauri 2 (Rust), reuses the web UI |
| Android and iOS app | [mobile](mobile) | Kotlin, Compose Multiplatform |

The server stores everything in a single data folder (a SQLite database plus
the audio files) and needs no other database or service. It can also serve
the web app itself, so a whole installation is one process.

## Features

- **Library:** upload MP3, WAV, FLAC, OGG and M4A. Missing tags and covers are
  looked up on MusicBrainz, Deezer and iTunes, lyrics come from the file or
  from LRCLIB, and loudness, waveform and silence trimming run in the
  background when ffmpeg is installed.
- **Playlists:** private or public, with your own cover, pinned to the
  sidebar, plus favorites, history, albums and artists.
- **Playback:** queue, gapless or crossfade, volume normalisation, sleep
  timer, synced lyrics, and offline downloads in the desktop, Android and
  iOS apps.
- **Together:** listening sessions that play the same queue in sync, and
  share links that play one track for someone without an account.
- **Accounts:** the first account becomes the admin. Sign-ups can be open,
  closed, or by single-use invite link, and an admin can issue a one-time
  password reset link. Storage can be capped per user.
- **Discover:** search other sources from the app and add tracks to your
  library. See [Third-party sources](#third-party-sources).

## Run it

You need [Go](https://go.dev/dl/) (the version in
[go.mod](backend/music-server/go.mod)) and [Bun](https://bun.sh). Install
[ffmpeg](https://ffmpeg.org) to get format conversion for iOS, loudness
analysis and waveforms; the server works without it.

```sh
cp .env.example .env

# Terminal 1: the server, on port 3001
cd backend/music-server
go run .

# Terminal 2: the web app, on port 3000
bun install
bun run dev
```

Open <http://localhost:3000> and create an account. It is the admin account.

The server reads `.env` from its working directory or any folder above it, so
the one at the repository root serves both. Every option is documented in
[.env.example](.env.example).

## Deploy it

Put the server behind a reverse proxy that provides HTTPS (Caddy, nginx or
Traefik all work). The lightest setup is one process:

```sh
bun run build:static                # writes the web app to out/
cd backend/music-server
go build -o music-server .
MUSIC_WEB_DIR=../../out ./music-server
```

Send everything, `/api` included, to the server's port. The alternative is
`bun run build` and `bun run start`, which runs the web app as a second
process that forwards `/api` to the Go server.

The data folder (`MUSIC_DATA_DIR`) is the only thing to back up. A first
session secret is generated into it; set `MUSIC_JWT_SECRET` if you rebuild
the folder and want existing sessions to survive.

If you lock yourself out, `music-server -reset-password <username>` prints a
one-time reset link for that account.

## Apps

- [Desktop](desktop/README.md): installers for Windows and Linux.
- [Android and iOS](mobile/README.md): build instructions, signing,
  TestFlight.

The apps ask for your server's address on the sign-in screen. To hand out
builds that already know it, and to fill in the details that belong to you:

| What | Setting |
|---|---|
| Web and desktop: default server | `NEXT_PUBLIC_MUSIC_DEFAULT_SERVER` at build time |
| Privacy policy page | `NEXT_PUBLIC_MUSIC_OPERATOR` and `NEXT_PUBLIC_MUSIC_CONTACT` at build time |
| Android: default server and link domain | `musicHost` in `~/.gradle/gradle.properties` |
| iOS: default server and link domain | `MUSIC_HOST` in `mobile/iosApp/Configuration/*.xcconfig` |

Leave the privacy settings empty and the page says the operator hasn't
published a policy. The text that appears once you set them is written for a
server in the EU under the GDPR; read it and adapt it to your situation
before you publish it.

The application ID `com.cornspies.music`, the Apple team ID in
`public/.well-known/apple-app-site-association` and the signing fingerprints
in `public/.well-known/assetlinks.json` belong to the original author. If you
publish your own builds, change them to yours.

## Third-party sources

Discover can search several outside services. Check that using each one is
allowed where you live and under that service's terms; you are responsible
for what you add to your library.

- **NoCopyrightSounds:** free to use with credit.
- **SoundCloud:** tracks SoundCloud plays in full to signed-out listeners.
  It uses SoundCloud's web API, so it can break when their site changes.
- **YouTube:** off unless you point `MUSIC_YTDLP` at a
  [yt-dlp](https://github.com/yt-dlp/yt-dlp) binary. Downloading from YouTube
  is against its terms of service.
- **Deezer:** searches the public catalogue. Downloading audio from Deezer is
  against its terms of service; the higher qualities need your own Deezer
  account (`MUSIC_DEEZER_ARL`).

Set `MUSIC_DISABLE_DISCOVER=1` to remove Discover altogether, or
`MUSIC_DISABLE_DEEZER=1` and `MUSIC_DISABLE_SOUNDCLOUD=1` to switch off
individual sources.

If you have the means, please consider buying the music you like from the
artist or their label instead. It is the most direct way to support them.

## Develop

```sh
cd backend/music-server && go vet ./... && go test ./...
bun run typecheck && bun run lint
cd mobile && ./gradlew :androidApp:assembleDebug
```

## Support the author

If you like this project and want to support it, you can sponsor me on
[GitHub Sponsors](https://github.com/sponsors/Cornspies). It is entirely
optional.

## License

[MIT](LICENSE)
