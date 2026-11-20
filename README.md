# Music

> **Disclaimer: this project was developed with the help of AI.**

A self-hosted music server with apps for the web, Windows, Linux, Android and
iOS. You run one server, upload your own music, and listen from anywhere.

| Part | Folder | Built with |
|---|---|---|
| Server | [backend/music-server](backend/music-server) | Go, SQLite |
| Web app | [src](src) | Next.js, React, Tailwind |
| Desktop app | [desktop](desktop) | Tauri 2 (Rust), reuses the web UI |
| Android and iOS app | [mobile](mobile) | Kotlin, Compose Multiplatform |

Everything the server stores is in one data folder: a SQLite database plus the
audio files. It serves the web app too, so an installation is one process.

## Features

- **Library:** upload MP3, WAV, FLAC, OGG and M4A. Missing tags, covers and
  lyrics are looked up for you.
- **Playback:** queue, gapless or crossfade, volume normalisation, sleep
  timer, synced lyrics, and offline downloads in the apps.
- **Playlists:** private or public, plus favorites, history, albums and
  artists.
- **Together:** listening sessions that play the same queue in sync, and
  share links that play one track for someone without an account.
- **Accounts:** the first account is the admin. Sign-ups can be open, closed
  or by invite link, and storage can be capped per user.
- **Discover:** search other sources from the app and add tracks to your
  library. [Check their terms first](docs/sources.md).

More about each in [docs/features.md](docs/features.md).

## Quick install

Download the server for your system from the
[Releases](https://github.com/Cornspies/music-player/releases) page:
`Music-server-<version>-linux-amd64.tar.gz`, `…-linux-arm64.tar.gz` or
`…-windows-amd64.zip`.

**Linux**

```sh
tar -xzf Music-server-*-linux-amd64.tar.gz
cd Music-server-*-linux-amd64
cp .env.example .env
./music-server
```

**Windows:** extract the zip, open PowerShell in the extracted folder, then run:

```powershell
copy .env.example .env
.\music-server.exe
```

Open <http://localhost:3001> and create an account. The first account is the
admin.

- The server reads the `.env` next to it and keeps everything in `data/`
  beside it. Every option is explained in [.env.example](.env.example).
- Other devices use `http://<this machine's address>:3001`. To reach it over
  the internet, put it behind HTTPS: see [Deploy it](docs/server.md#deploy-it).
- ffmpeg is optional. Install it for format conversion for iOS, loudness
  analysis and waveforms.

## Run from source

You need [Go](https://go.dev/dl/) and [Bun](https://bun.sh).

```sh
cp .env.example .env

# Terminal 1: the server, on port 3001
cd backend/music-server
go run .

# Terminal 2: the web app, on port 3000
bun install
bun run dev
```

Open <http://localhost:3000> and create an account.

## More

- [Running a server](docs/server.md): as a Linux service, updates and
  rollback, HTTPS, the maintenance page, monitoring.
- [Desktop app](desktop/README.md) and [Android and iOS app](mobile/README.md).
- [Your own builds](docs/apps.md): default server, privacy policy page, app
  IDs.
- [Third-party sources](docs/sources.md) that Discover can search.
- [Developing and releasing](docs/development.md): running from source,
  checks, the release workflow.

## Support the author

If you like this project and want to support it, you can sponsor me on
[GitHub Sponsors](https://github.com/sponsors/Cornspies). It is entirely
optional.

## License

[MIT](LICENSE)
