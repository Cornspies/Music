# Music — Compose Multiplatform mobile app

Android + iOS client for the Music personal music streaming platform
(Go backend + Next.js web app live in the parent repository).

One shared Kotlin UI — built with [Compose Multiplatform](https://www.jetbrains.com/lp/compose-multiplatform/) —
running natively on both platforms:

| Platform | Player engine | Uploads |
|---|---|---|
| Android (minSdk 26) | Media3 ExoPlayer | System file picker (SAF) |
| iOS (15.6+) | AVFoundation AVPlayer | via web app (document picker TODO) |

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
- **Android Studio Ladybug+** (or plain Gradle 8.9+ with the Android SDK; compileSdk 35)
- **Xcode 15+** with an active command-line tools selection (iOS builds only)
- The **Music Go backend running** (see `../mini-services/music-server`)

## Configure the server URL

Set it once on the login screen — it's persisted per device:

| Environment | URL |
|---|---|
| Android emulator | `http://10.0.2.2:3001` (default) |
| iOS simulator | `http://localhost:3001` (default) |
| Physical device | `http://<YOUR-LAN-IP>:3001` (e.g. `http://192.168.1.20:3001`) |

Notes:
- The Android manifest sets `android:usesCleartextTraffic="true"` because streaming
  from a self-hosted LAN server over plain HTTP is the primary use case. For public
  deployments, front the Go server with TLS (e.g. Caddy) and use `https://`.
- iOS: `localhost` loopback is ATS-exempt. For LAN IPs add
  `NSAppTransportSecurity → NSAllowsLocalNetworking` to `iosApp/iosApp/Info.plist`.

## Build & run

### Android

```bash
# generate the gradle wrapper jar if missing (one-time)
gradle wrapper --gradle-version 8.9

# build the debug APK
./gradlew :composeApp:assembleDebug
# → composeApp/build/outputs/apk/debug/composeApp-debug.apk
```

Or simply open the project root in **Android Studio** and press ▶ on the
`composeApp` configuration.

### iOS

1. `cd iosApp && open iosApp.xcodeproj`
2. Select the `iosApp` scheme + an iPhone simulator
3. Press ⌘R — the `ComposeApp` framework target runs
   `./gradlew :composeApp:embedAndSignAppleFrameworkForXcode` automatically.

> The pbxproj mirrors the official Compose Multiplatform template. If Xcode
> complains (versions drift), recreate the shell: new iOS App named `iosApp` in this
> folder, then (a) add a **framework target** `ComposeApp` whose only build phase is
> the Run-Script from `project.pbxproj`, (b) link `ComposeApp` into the app target,
> (c) set `Info.plist` to `iosApp/Info.plist`.

## Project layout

```
composeApp/src/
├── commonMain/kotlin/com/music/app/
│   ├── App.kt                    # root composable + navigation + bottom tabs
│   ├── Platform.kt               # expect: file picker / canUpload / readBytes
│   ├── api/                      # Ktor ApiClient, @Serializable models, TokenStore expect
│   ├── state/AppModel.kt         # single observable state holder (auth, data, player queue)
│   ├── player/PlayerController.kt# expect: load/play/pause/seek + isPlaying/position flows
│   └── ui/                       # theme, components (TrackRow, CoverImage, NowPlayingBar…), screens
├── androidMain/                  # MainActivity, ExoPlayer, SharedPreferences, SAF picker
└── iosMain/                      # MainViewController, AVPlayer, NSUserDefaults
iosApp/                           # Xcode project (SwiftUI app shell)
```

## API

The client talks to the Go backend documented in `../worklog.md` (API contract):
JWT bearer for JSON calls, `?token=` query for stream/cover URLs (media players
can't send headers).
