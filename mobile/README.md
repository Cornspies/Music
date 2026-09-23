# Music — Compose Multiplatform mobile app

Android + iOS client for the Music personal music streaming platform
(Go backend + Next.js web app live in the parent repository).

One shared Kotlin UI — built with [Compose Multiplatform](https://www.jetbrains.com/lp/compose-multiplatform/) —
running natively on both platforms:

| Platform | Player engine | Uploads |
|---|---|---|
| Android (minSdk 27) | Media3 ExoPlayer in a MediaSessionService | System file picker (SAF) |
| iOS (15.6+) | AVFoundation AVPlayer | Files document picker |

## Features

- Login / register against your Music server (server URL configurable on the login screen, persisted)
- Library with sorting (recent / title / artist / plays)
- Home: stats, recently added, most played
- Full player screen (seek, shuffle, repeat off/all/one, queue auto-advance) + mini now-playing bar
- Favorites (optimistic heart toggles), playlists (create / detail / add & remove tracks)
- Debounced search across tracks, albums, artists, playlists
- Cover art with gradient fallbacks, MediaSession-style dark theme (emerald on zinc)

## Prerequisites

- **JDK 17** (Temurin/Corretto)
- **Android Studio Otter 3+** (or plain Gradle 9.7+ with the Android SDK; compileSdk 37)
- **Xcode 15+** with an active command-line tools selection (iOS builds only).
  Simulator builds are Apple-silicon only (`iosSimulatorArm64`): Compose
  Multiplatform no longer ships `iosX64` artifacts.
- The **Music Go backend running** (see `../mini-services/music-server`)

## Configure the server URL

A fresh installation points at `https://music.cornspies.com`; change it on
the login screen and it is persisted per device. Point your own builds
somewhere else by editing `defaultBaseUrl` in
`composeApp/src/{androidMain,iosMain}/kotlin/com/music/app/TokenStore.*.kt`.

| Environment | URL |
|---|---|
| Installed app | `https://music.cornspies.com` (default) |
| Android emulator → server on this machine | `http://10.0.2.2:3001` |
| iOS simulator → server on this machine | `http://localhost:3001` |
| Physical device → server on this machine | `http://<YOUR-LAN-IP>:3001` (e.g. `http://192.168.1.20:3001`) |

Enter the site's origin, **not** its API path — the app appends `/api/…`
itself, so `https://music.cornspies.com` is right and
`https://music.cornspies.com/api` would ask the server for `/api/api/…`.
What you type is used as it stands, so a server that really does live under
a path works too.

Notes:
- Plain `http://` is accepted for **private addresses only** (localhost, the
  emulator host, 10/8, 172.16/12, 192.168/16, `.local`). Anything else over
  http shows a warning on the sign-in screen and needs an explicit "I
  understand" — your password and session would travel unencrypted. For
  public deployments, front the Go server with TLS (e.g. Caddy) and use
  `https://`. On iOS the same rule is enforced by App Transport Security
  (`NSAllowsLocalNetworking` in `Info.plist`).
- The session token is stored in the Android Keystore (AES-GCM, hardware
  backed where available) and the iOS Keychain (`AfterFirstUnlockThisDeviceOnly`).
  Backups are disabled on Android; the Keychain item never leaves the device.

## Background playback, lock screen, widget

- **Android**: playback runs in `PlaybackService` (a Media3 `MediaSessionService`),
  so it survives the activity, shows a media notification with artwork and
  transport controls, and answers headset/Bluetooth/Android Auto. Android 13+
  asks for the notification permission on first launch. A **home-screen widget**
  ("Now playing", Glance) shows the track with play/pause/skip.
- **iOS**: the audio session is set to playback, what's playing is published to
  the lock screen / Control Centre, and the remote command centre drives
  play/pause/skip/scrub. A **WidgetKit** widget lives in `iosApp/MusicWidget/`;
  it needs a one-time Xcode setup:
  1. File → New → Target → Widget Extension, name it "MusicWidget", untick
     "Include Configuration Intent", and replace its generated Swift file with
     `MusicWidget.swift`.
  2. Add the App Groups capability to both the iosApp and MusicWidget targets
     with the group `group.com.music.app` (it matches `WIDGET_APP_GROUP` in
     `PlayerController.ios.kt`).

## Offline

- Any track or playlist can be downloaded from its row/screen.
- A downloaded playlist can be marked **Keep offline**: it is re-synced on every
  launch (and from *Downloads → Sync now*), so tracks added on another device
  arrive and removed ones go. Metadata edits made anywhere are picked up via
  the server's delta endpoint (`GET /api/tracks/changes?since=`).
- **Download on Wi-Fi only** (Downloads screen) holds audio downloads back on
  mobile data; the playlist track lists still sync.

## Shared links

`https://<musicHost>/playlists/<id>`, shared tracks (`/s/<token>`) and invite
links (`/register`) open straight in the app once Android has verified that
the app belongs to the domain. For that, `public/.well-known/assetlinks.json`
in the repository root must list the SHA-256 fingerprint of the certificate
the installed APK was signed with. It lists the release key and the debug key
of the machine the app was developed on; to read another APK's fingerprint:

```bash
$ANDROID_HOME/build-tools/<version>/apksigner verify --print-certs Music-<version>.apk
```

Android checks the file when the app is installed or updated. After changing
it, redeploy the web app, then reinstall the app or run
`adb shell pm verify-app-links --re-verify com.music.app`;
`adb shell pm get-app-links com.music.app` shows the result. iOS needs your
Team ID in place of `REPLACE_WITH_TEAMID` in `apple-app-site-association`.

## Build & run

### Android

```bash
# generate the gradle wrapper jar if missing (one-time)
gradle wrapper --gradle-version 9.7.1

# build the debug APK
./gradlew :androidApp:assembleDebug
# → androidApp/build/outputs/apk/debug/androidApp-debug.apk
```

Or simply open the project root in **Android Studio** and press ▶ on the
`androidApp` configuration.

#### Release build for the server's download list

```bash
./gradlew :androidApp:releaseApk
# → mobile/dist/Music-<versionName>.apk
```

That is the name the server's "Android app" list expects, so the file can be
copied straight into its APK directory (`MUSIC_APK_DIR`, default
`<data dir>/apk`). Raise `versionName`/`versionCode` in
`androidApp/build.gradle.kts` before each build; both files can sit in the
directory, and the newest version is offered first.

**Signing is required** — an unsigned APK will not install, and Android only
accepts an update when it carries the same key as the version already on the
phone. Create a key once and keep it safe; losing it means every installation
has to be uninstalled before it can be updated.

```bash
keytool -genkeypair -v -keystore ~/music-release.jks -alias music \
        -keyalg RSA -keysize 2048 -validity 10000
```

Then either write `mobile/keystore.properties` (gitignored):

```properties
storeFile=/home/you/music-release.jks
storePassword=...
keyAlias=music
keyPassword=...
```

…or pass the same values on the command line:

```bash
./gradlew :androidApp:releaseApk -Pmusic.storeFile=$HOME/music-release.jks \
    -Pmusic.storePassword=... -Pmusic.keyAlias=music -Pmusic.keyPassword=...
```

Without either, the build still runs and the task warns that the APK is
unsigned.

### iOS

The Xcode project is generated, not committed: `iosApp/project.yml` is an
[XcodeGen](https://github.com/yonaskolb/XcodeGen) spec mirroring the official
Compose Multiplatform template.

```bash
brew install xcodegen
cd iosApp && xcodegen generate && open iosApp.xcodeproj
```

Select the `iosApp` scheme + an iPhone simulator and press ⌘R — the "Compile
Kotlin Framework" phase runs `./gradlew :composeApp:embedAndSignAppleFrameworkForXcode`
before the Swift sources build.

**No Mac?** `.github/workflows/ios.yml` does the same on a GitHub-hosted macOS
runner (on pushes touching `mobile/`, or by hand from the Actions tab) and
uploads the simulator build as an artifact. macOS minutes are billed at 10x,
so the path filter matters on a private repo.

One-time setup in Xcode (Signing & Capabilities), once you have a Team ID —
put it in `iosApp/Configuration/*.xcconfig` as `TEAM_ID`:

- **Associated Domains** → `applinks:<musicHost>` so `https://<host>/playlists/…`
  and invite links open the app. The `music://playlist/<id>` scheme used by
  the app's own share button needs nothing — it's registered in `Info.plist`.
- **App Groups** → `group.com.music.app` on both the app and the widget
  target, then add `MusicWidget/` as a Widget Extension target.

## Project layout

```
androidApp/                       # Android application shell: applicationId, version, deep-link host, app-level manifest — no code
composeApp/src/                   # everything else, as a Kotlin Multiplatform library (Android library + iOS framework)
├── commonMain/kotlin/com/music/app/
│   ├── App.kt                    # root composable + navigation + bottom tabs
│   ├── Platform.kt               # expect: file picker / canUpload / readBytes
│   ├── api/                      # Ktor ApiClient, @Serializable models, TokenStore expect
│   ├── state/AppModel.kt         # single observable state holder (auth, data, player queue)
│   ├── player/PlayerController.kt# expect: load/play/pause/seek + isPlaying/position flows
│   └── ui/                       # theme, components (TrackRow, CoverImage, NowPlayingBar…), screens
├── androidMain/                  # MainActivity, PlaybackService (Media3 session), Keystore token store, SAF picker, Glance widget
└── iosMain/                      # MainViewController, AVPlayer + now-playing/remote commands, Keychain token store, document/photo pickers
iosApp/                           # SwiftUI app shell + XcodeGen spec (project.yml); MusicWidget/ (WidgetKit source, needs a target)
```

## API

The client talks to the Go backend documented in `../worklog.md` (API contract):
JWT bearer for JSON calls. Stream/cover/avatar URLs carry a short-lived,
read-only **media token** (`GET /api/auth/media-token`, refreshed by the app)
in `?token=` because media players can't send headers — a URL that ends up in
a log or share sheet is worth a few hours of listening, not the account.
"Sign out everywhere" (`POST /api/auth/logout-all`) and password changes
revoke every session.
