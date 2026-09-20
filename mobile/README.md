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

Set it once on the login screen — it's persisted per device:

| Environment | URL |
|---|---|
| Android emulator | `http://10.0.2.2:3001` (default) |
| iOS simulator | `http://localhost:3001` (default) |
| Physical device | `http://<YOUR-LAN-IP>:3001` (e.g. `http://192.168.1.20:3001`) |

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
  it needs a one-time Xcode step (add a Widget Extension target and the
  `group.com.music.app` App Group to both targets — see the comments at the top
  of `MusicWidget.swift`).

## Offline

- Any track or playlist can be downloaded from its row/screen.
- A downloaded playlist can be marked **Keep offline**: it is re-synced on every
  launch (and from *Downloads → Sync now*), so tracks added on another device
  arrive and removed ones go. Metadata edits made anywhere are picked up via
  the server's delta endpoint (`GET /api/tracks/changes?since=`).
- **Download on Wi-Fi only** (Downloads screen) holds audio downloads back on
  mobile data; the playlist track lists still sync.

## Shared playlist links

The web app serves `/.well-known/assetlinks.json` and
`apple-app-site-association`. Fill in your signing certificate fingerprint /
Team ID there (see `public/.well-known/README.md` in the repository root) and
`https://<musicHost>/playlists/<id>` opens straight in the app.

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

### iOS

1. `cd iosApp && open iosApp.xcodeproj`
2. Select the `iosApp` scheme + an iPhone simulator
3. Press ⌘R — the `ComposeApp` framework target runs
   `./gradlew :composeApp:embedAndSignAppleFrameworkForXcode` automatically.

One-time setup in Xcode (Signing & Capabilities), once you have a Team ID:

- **Associated Domains** → `applinks:<musicHost>` so `https://<host>/playlists/…`
  and invite links open the app. The `music://playlist/<id>` scheme used by
  the app's own share button needs nothing — it's registered in `Info.plist`.
- **App Groups** → `group.com.music.app` on both the app and the widget
  target, then add `MusicWidget/` as a Widget Extension target.

> The pbxproj mirrors the official Compose Multiplatform template. If Xcode
> complains (versions drift), recreate the shell: new iOS App named `iosApp` in this
> folder, then (a) add a **framework target** `ComposeApp` whose only build phase is
> the Run-Script from `project.pbxproj`, (b) link `ComposeApp` into the app target,
> (c) set `Info.plist` to `iosApp/Info.plist`.

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
iosApp/                           # Xcode project (SwiftUI app shell) + MusicWidget/ (WidgetKit source, needs a target)
```

## API

The client talks to the Go backend documented in `../worklog.md` (API contract):
JWT bearer for JSON calls. Stream/cover/avatar URLs carry a short-lived,
read-only **media token** (`GET /api/auth/media-token`, refreshed by the app)
in `?token=` because media players can't send headers — a URL that ends up in
a log or share sheet is worth a few hours of listening, not the account.
"Sign out everywhere" (`POST /api/auth/logout-all`) and password changes
revoke every session.
