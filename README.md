# Music

> **Disclaimer: this project was developed with the help of AI.**

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

## Quick install

Download the server for your system from the
[Releases](https://github.com/Cornspies/music-player/releases) page:
`Music-server-<version>-linux-amd64.tar.gz`, `…-linux-arm64.tar.gz` (a
Raspberry Pi, for example) or `…-windows-amd64.zip`.

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
admin. The folder holds the server, the web app (`web/`) and an `.env.example`
that is ready to use as it is; every option in it is explained.

- **It stays in its folder.** The server reads the `.env` next to it and keeps
  everything it stores in `data/` beside it, wherever it is started from.
- **Other devices** use `http://<this machine's address>:3001`, which the apps
  ask for when you sign in. To reach it over the internet, put it behind a
  reverse proxy with HTTPS (see [Deploy it](#deploy-it)).
- **ffmpeg** is optional. Install it for format conversion for iOS, loudness
  analysis and waveforms.
- **To update,** stop the server, back up `data/`, delete `web/` and extract
  the new release into the same folder (on Linux, `tar -xzf <file>
  --strip-components=1 -C <folder>`). `.env` and `data/` aren't part of a
  release, so they are kept. Run as a Linux service, `music-update` does all
  of this for you (see [Updates](#updates)).
- The privacy page says no policy has been published until you set
  `MUSIC_OPERATOR` and `MUSIC_CONTACT` in `.env` (see [Apps](#apps)).

### Run as a service (Linux)

The Linux bundles include `music-server.service`, a hardened systemd unit for a
server in `/opt/music` that runs as its own `music` user:

```sh
sudo useradd --system --home-dir /opt/music --shell /usr/sbin/nologin music
sudo mkdir -p /opt/music
sudo tar -xzf Music-server-*-linux-amd64.tar.gz --strip-components=1 -C /opt/music
sudo cp /opt/music/.env.example /opt/music/.env
sudo chmod 600 /opt/music/.env
sudo chown -R music:music /opt/music
sudo cp /opt/music/music-server.service /etc/systemd/system/
sudo cp /opt/music/music-update.sh /usr/local/bin/music-update
sudo systemctl enable --now music-server
```

Logs are in `/opt/music/data/logs`: a full log per day and, on days with
errors, a `music-server-errors-<date>.log` with only those.

### Updates

`music-update` updates a server installed as above:

```sh
sudo music-update              # the latest release
sudo music-update v0.1.3       # a particular release
sudo music-update rollback     # back to the release before
music-update status            # what's installed and what's available
```

It downloads the bundle for your machine from GitHub and checks it against
the SHA-256 GitHub lists for it. Then it stops the server, copies the database
to `/opt/music/backups` (the last five are kept), swaps in the new server and
web app, and starts it again. If the new server doesn't answer `/healthz`
within two minutes, it goes back to the old release and the database from
before on its own. `.env`, `data/` and your systemd unit stay as they are; if a
release changes the unit, it tells you. With the [maintenance
page](#maintenance) set up, users see "down for maintenance" while it runs,
and the script keeps the page and `music-maintenance` up to date.

When `.env` sets `MUSIC_APK_DIR`, it also downloads the release's Windows
installer into that folder (checked the same way), so the server offers it
under "Other platforms" and desktop apps on Windows update to it. Older
Windows installers there are removed; one of the same name but different
contents, such as your own build, is kept. Running `music-update` when you're
already on the latest release fetches it if it's missing.

`rollback` keeps the current database and switches between the two most
recent releases, so running it again returns to the newer one. To install a
bundle you downloaded yourself, pass its file instead of a version. Set
`MUSIC_DIR` or `MUSIC_SERVICE` when the server lives elsewhere or its service
has another name.

On a server that predates the script, install it once from GitHub:

```sh
sudo curl -fsSL -o /usr/local/bin/music-update \
  https://raw.githubusercontent.com/Cornspies/Music/main/deploy/music-update.sh
sudo chmod 755 /usr/local/bin/music-update
```

### Monitoring

Set `MUSIC_METRICS_ADDR` (for example `127.0.0.1:9464`) to serve Prometheus
metrics at `/metrics` on a separate port: requests by route and status,
latency, errors, plays, library size, background queues, and Go and process
stats. Set `MUSIC_METRICS_TOKEN` as well when Prometheus scrapes from another
machine. `/healthz` on the main port answers `{"status":"ok"}` for uptime
checks.

## Run from source

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
The server logs that `web/` has no `index.html`; that is expected while you
develop, because the web app on port 3000 serves itself.

The server reads `.env` from its working directory or any folder above it, so
the one at the repository root serves both. Every option is documented in
[.env.example](.env.example).

## Deploy it

Put the server behind a reverse proxy that provides HTTPS (Caddy, nginx or
Traefik all work), and send everything, `/api` included, to the server's
port. For nginx, [deploy/nginx/music.conf](deploy/nginx/music.conf) is a
complete site with a maintenance switch (see [Maintenance](#maintenance)).

To build and serve everything from source, one process serves both:

```sh
bun run build:static                # writes the web app to out/
cd backend/music-server
go build -o music-server .
MUSIC_WEB_DIR=../../out ./music-server
```

The alternative is `bun run build` and `bun run start`, which runs the web app
as a second process that forwards `/api` to the Go server. Remove
`MUSIC_WEB_DIR` from `.env` when you use it.

The data folder (`MUSIC_DATA_DIR`) is the only thing to back up. A first
session secret is generated into it; set `MUSIC_JWT_SECRET` if you rebuild
the folder and want existing sessions to survive.

If you lock yourself out, `music-server -reset-password <username>` prints a
one-time reset link for that account.

### Maintenance

With nginx in front, you can tell people when Music will be back while you
work on the server. The files are in [deploy/nginx](deploy/nginx), and in the
`nginx` folder of the Linux bundles. Set them up once:

```sh
sudo cp -r nginx/maintenance /var/www/music-maintenance
sudo cp nginx/maintenance.sh /usr/local/bin/music-maintenance
sudo cp nginx/music.conf /etc/nginx/conf.d/music.conf   # then set server_name and the certificates
sudo nginx -t && sudo systemctl reload nginx
```

Then, before you stop the server:

```sh
sudo music-maintenance on 30m                          # or 2h, 1h30m
sudo music-maintenance on 2h "Moving to a new disk."   # with a note for everyone
sudo music-maintenance off                             # when you're done
```

It takes effect at once; nginx doesn't need a reload. While it's on, nginx
answers every request itself:

- The web app shows a banner, "Music is down for maintenance, back around
  21:30", in each person's own time zone, with your note. Downloaded tracks
  keep playing. Anyone who opens the site gets a maintenance page instead.
- The desktop and mobile apps show the same banner and switch to their
  downloads.
- All of them check back every 30 to 60 seconds and pick up where they were
  once you turn maintenance off. The maintenance page reloads itself.

If the server stops without maintenance being on, the apps say they can't
reach the server and keep checking back, and the site shows a "not
responding" page. Nobody is signed out either way.

## Apps

- [Desktop](desktop/README.md): installers for Windows and Linux.
- [Android and iOS](mobile/README.md): build instructions, signing,
  TestFlight.

The apps work with any server: they ask for its address on the sign-in
screen, and shared links from any server open in them through the web app's
**Open in the app** button. Links open in the app directly, without the
browser, only for the domain a build was made for. To hand out builds that
already know your server, and to fill in the details that belong to you:

| What | Setting |
|---|---|
| Privacy policy page | `MUSIC_OPERATOR` and `MUSIC_CONTACT` in the server's `.env` |
| Desktop: default server | `NEXT_PUBLIC_MUSIC_DEFAULT_SERVER` at build time |
| Android: default server and link domain | `musicHost` in `~/.gradle/gradle.properties` |
| iOS: default server and link domain | `MUSIC_HOST` in `mobile/iosApp/Configuration/*.xcconfig` |

The server reads the privacy settings, so release builds work for any
operator. Leave them empty and the page says the operator hasn't published a
policy. The text that appears once you set them is written for a server in
the EU under the GDPR; read it and adapt it to your situation before you
publish it.

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

**If you have the means, please consider buying the music you like from the
artist or their label instead. It is the most direct way to support them.**

## Develop

```sh
cd backend/music-server && go vet ./... && go test ./...
bun run typecheck && bun run lint
cd mobile && ./gradlew :androidApp:assembleDebug
```

To publish a release, run **Actions → Release → Run workflow** and enter a tag
such as `v0.4.4`. It builds the desktop installers, a signed Android APK and
the server bundles (server plus web app) for Linux and Windows, and attaches
them to a GitHub release, a draft by default. It needs the
repository secrets `ANDROID_KEYSTORE_BASE64` (`base64 -w0` of your keystore),
`ANDROID_STORE_PASSWORD`, `ANDROID_KEY_ALIAS` and `ANDROID_KEY_PASSWORD`, and
reads the repository variables `MUSIC_DEFAULT_SERVER` and `MUSIC_HOST`.

## Support the author

If you like this project and want to support it, you can sponsor me on
[GitHub Sponsors](https://github.com/sponsors/Cornspies). It is entirely
optional.

## License

[MIT](LICENSE)
