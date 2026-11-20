# Developing and releasing

## Run from source

You need [Go](https://go.dev/dl/) (the version in
[go.mod](../backend/music-server/go.mod)) and [Bun](https://bun.sh). Install
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
[.env.example](../.env.example).

## Checks

```sh
cd backend/music-server && go vet ./... && go test ./...
bun run typecheck && bun run lint
cd mobile && ./gradlew :androidApp:assembleDebug
```

## Releases

To publish a release, push a tag:

```sh
git tag v0.2.0 && git push origin v0.2.0
```

The Release workflow builds the desktop installers, a
signed Android APK and the server bundles (server plus web app) for Linux and
Windows, and publishes them as a GitHub release. The notes are the tagged
commit's message, or GitHub's generated notes when it has no body; a tag with
a hyphen, such as `v0.2.0-rc1`, becomes a pre-release. It needs the
repository secrets `ANDROID_KEYSTORE_BASE64` (`base64 -w0` of your keystore),
`ANDROID_STORE_PASSWORD`, `ANDROID_KEY_ALIAS` and `ANDROID_KEY_PASSWORD`, and
reads the repository variables `MUSIC_DEFAULT_SERVER` and `MUSIC_HOST`. With
the secret `TAURI_SIGNING_PRIVATE_KEY` and the variable
`MUSIC_UPDATE_PUBLIC_KEY` it also signs the desktop installers, which lets
the desktop app update itself (see [Desktop](desktop.md#updates)).

Before tagging, add the new version's key points to `src/lib/whats-new.ts`
(desktop) and `mobile/composeApp/src/commonMain/kotlin/com/music/app/WhatsNew.kt`
(Android and iOS), under the version number the build will carry. Each app
shows its points once, the first time it starts after updating to that
version; a version without an entry shows nothing.
