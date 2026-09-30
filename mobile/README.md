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
- Listen together: host a session, share its invite link, and everyone who
  joins (with an account on the server) hears the same queue in sync. The
  host decides whether everyone may add songs (a whole playlist at once, up
  to 1000 in the queue) and skip; pausing only ever pauses your own device
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
     with the group `group.com.cornspies.music` (it matches `WIDGET_APP_GROUP` in
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

`https://<musicHost>/playlists/<id>`, shared tracks (`/s/<token>`), invite
links (`/register`) and listening-session invites (`/listen/<code>`) open
straight in the app once Android has verified that the app belongs to the
domain. For that, `public/.well-known/assetlinks.json` in the repository root
must list the SHA-256 fingerprint of the certificate
the installed APK was signed with. It lists the release key and the debug key
of the machine the app was developed on; to read another APK's fingerprint:

```bash
$ANDROID_HOME/build-tools/<version>/apksigner verify --print-certs Music-<version>.apk
```

Android checks the file when the app is installed or updated. After changing
it, redeploy the web app, then reinstall the app or run
`adb shell pm verify-app-links --re-verify com.cornspies.music`;
`adb shell pm get-app-links com.cornspies.music` shows the result.

On iOS, `public/.well-known/apple-app-site-association` names the app by Team ID
and bundle ID and lists the paths it opens (`/playlists/`, `/s/`, `/listen/`),
and the app claims the domain in `iosApp/iosApp/iosApp.entitlements`. iOS reads
the file through Apple's CDN when the app is installed or updated, so redeploy
the web app first; `https://app-site-association.cdn-apple.com/a/v1/<musicHost>`
shows the copy Apple has, which can lag a day behind.

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
runner and uploads the simulator build as an artifact. Like every workflow
here it runs only when started from the Actions tab (**Run workflow**) —
macOS minutes are billed at 10x. To get the app onto an iPhone, see
[TestFlight](#testflight) below.

To sign builds from Xcode, put your Team ID in `iosApp/Configuration/*.xcconfig`
as `TEAM_ID` (the TestFlight workflow takes it from the provisioning profile).

The app's capabilities are in `iosApp/iosApp/iosApp.entitlements`, and the App
ID must have each of them (see [TestFlight](#testflight) step 1):

- **Associated Domains** → `applinks:music.cornspies.com`, so shared links
  (see [Shared links](#shared-links)) open the app. The `music://playlist/<id>`
  scheme used by the app's own share button needs nothing — it's registered
  in `Info.plist`.

The widget isn't part of the project yet: it needs **App Groups** →
`group.com.cornspies.music` on both the app and a widget target, then
`MusicWidget/` added as a Widget Extension target.

#### TestFlight

`.github/workflows/testflight.yml` builds a signed Release build on a
GitHub-hosted Mac and uploads it to App Store Connect. Start it from the
Actions tab (**TestFlight → Run workflow**). Apple then processes the build
for 10–30 minutes, after which it shows up in the TestFlight app on the
iPhone. The run number is the build number (`1.0 (7)` came from run 7); raise
`MARKETING_VERSION` in `iosApp/project.yml` to start a new version.
TestFlight builds expire after 90 days.

One-time setup — a browser and a few `openssl` commands, no Mac needed:

1. **App ID.** In [Certificates, Identifiers & Profiles](https://developer.apple.com/account/resources/identifiers/list),
   add an App ID (type *App*) with the explicit bundle ID `com.cornspies.music`
   — the `PRODUCT_BUNDLE_IDENTIFIER` in `iosApp/project.yml` — and turn on
   **Associated Domains**, which `iosApp/iosApp/iosApp.entitlements` asks for.
   (The Android app uses the same ID.) Changing an App ID's capabilities
   invalidates its profiles: then redo step 3 and update
   `IOS_PROVISIONING_PROFILE` (step 6).
2. **Distribution certificate.** Create a private key and a signing request
   outside the repository — the key is the certificate's secret half, so keep
   it safe and never commit it:
   ```bash
   mkdir -p ~/ios-signing && cd ~/ios-signing
   openssl req -new -newkey rsa:2048 -nodes -keyout ios_distribution.key \
       -out ios_distribution.csr -subj "/emailAddress=you@example.com/CN=Your Name"
   ```
   Upload `ios_distribution.csr` under Certificates → + → **Apple
   Distribution**, save the downloaded `distribution.cer` next to it, and
   combine the two into a `.p12` (it asks for an export password):
   ```bash
   openssl x509 -inform DER -in distribution.cer -out distribution.pem
   openssl pkcs12 -export -inkey ios_distribution.key -in distribution.pem \
       -out ios_distribution.p12 -keypbe PBE-SHA1-3DES -certpbe PBE-SHA1-3DES -macalg sha1
   ```
   The last three options select the older encryption that the macOS
   keychain tools reliably import. To check the result, run
   `openssl pkcs12 -in ios_distribution.p12 -info -noout`: after the
   password it must list `MAC: sha1` and `pbeWithSHA1And3-KeyTripleDES-CBC`.
3. **Provisioning profile.** Profiles → + → Distribution → **App Store
   Connect**, choose the App ID and the certificate, and download the
   `.mobileprovision`.
4. **App record.** In [App Store Connect](https://appstoreconnect.apple.com/apps),
   + → New App: iOS, the bundle ID from step 1, any SKU, and a name that is
   still free on the App Store (it needn't match the home-screen name "Music").
5. **API key.** App Store Connect → Users and Access → Integrations → App
   Store Connect API → Team Keys → +, with any name and the **App Manager**
   role (the first visit asks you to enable API access). The workflow logs in
   with three things from that page:
   - the **Issuer ID**, shown above the list of keys — a UUID such as
     `57246542-96fe-1a63-e053-0824d011072a`, the same for every key of your
     team;
   - the **Key ID**, in the new key's row — ten characters such as
     `2X9R4HXF34`, also part of the file name `AuthKey_<Key ID>.p8`;
   - the `.p8` file itself — download it right away, Apple offers it only once.
6. **Repository secrets.** GitHub → Settings → Secrets and variables →
   Actions → New repository secret:

   | Secret | Value |
   |---|---|
   | `IOS_DISTRIBUTION_CERTIFICATE` | output of `base64 -w0 ios_distribution.p12` |
   | `IOS_DISTRIBUTION_CERTIFICATE_PASSWORD` | the `.p12` export password |
   | `IOS_PROVISIONING_PROFILE` | output of `base64 -w0 <name>.mobileprovision` |
   | `APP_STORE_CONNECT_ISSUER_ID` | the Issuer ID |
   | `APP_STORE_CONNECT_KEY_ID` | the Key ID |
   | `APP_STORE_CONNECT_PRIVATE_KEY` | the whole `.p8` file, `BEGIN`/`END` lines included |

   Don't select these values in the terminal: they are thousands of
   characters on one line, and part of them easily gets lost on the way.
   Pipe each one to the clipboard and paste that (`wl-copy` and `wl-paste`
   come with the `wl-clipboard` package; on X11 use
   `xclip -selection clipboard`):
   ```bash
   base64 -w0 ios_distribution.p12 | wl-copy       # IOS_DISTRIBUTION_CERTIFICATE
   base64 -w0 <name>.mobileprovision | wl-copy     # IOS_PROVISIONING_PROFILE
   wl-copy < AuthKey_<Key ID>.p8                   # APP_STORE_CONNECT_PRIVATE_KEY
   ```
   Run one line, paste into its secret straight away, then copy the next
   line — copying a command to run it replaces what's on the clipboard. To
   copy and check in one go:
   `base64 -w0 ios_distribution.p12 | wl-copy && wl-paste -n | base64 -d | cmp - ios_distribution.p12 && echo OK`.
   The GitHub CLI skips the clipboard entirely:
   `base64 -w0 ios_distribution.p12 | gh secret set IOS_DISTRIBUTION_CERTIFICATE`.

7. **Testers.** Once the first build is processed: the app in App Store
   Connect → TestFlight → Internal Testing → +, create a group with automatic
   distribution and add yourself. Install **TestFlight** from the App Store on
   the iPhone and accept the invitation; later builds arrive by themselves.

The certificate and the profile are valid for a year. When they expire,
repeat steps 2 and 3 and replace the three `IOS_*` secrets.

#### Testers outside your team

Internal testers must be users of your App Store Connect team (up to 100).
Anyone else — up to 10,000 people — can be an **external tester** with just
an Apple ID and the TestFlight app: App Store Connect → the app → TestFlight
→ External Testing → +, create a group and add a build, then invite people
by email or turn on a **public link**. The first build given to external
testers goes through Apple's beta review (usually about a day; later builds
normally don't), and it needs the group's **Test Information** filled in: a
description, a feedback email and, since the app starts at a sign-in screen,
a demo account for the reviewer (below). Every tester also needs an account
on your server, so send them an invite link as usual.

#### Demo account for Apple's review

A **demo account** has a library of its own: it starts empty and sees only
what it uploads itself. Regular accounts never see its uploads or playlists;
admins do, alongside everything else, so you can follow what the reviewer
does (and edit or delete it). Everything else works as for any account, and
a share link it creates still plays for whoever gets the link.

1. Admin → Invite links: create one and register the account with it
   (e.g. `appreview`, with a long random password).
2. Admin → Users → the account's menu → **Make demo account**.
3. Give Apple the username and password: in **Beta App Review Information**
   of the TestFlight test information, and later in **App Review
   Information** on the version you submit for the App Store.

Deleting a demo account deletes its uploads with it. Reviewers sometimes
test account deletion, so if the account is gone, create a new one before
the next review. **Make regular user** turns it back into a normal account,
and its uploads then join everyone's library.

## Project layout

```
androidApp/                       # Android application shell: applicationId, version, deep-link host, app-level manifest — no code
composeApp/src/                   # everything else, as a Kotlin Multiplatform library (Android library + iOS framework)
├── commonMain/kotlin/com/music/app/
│   ├── App.kt                    # root composable + navigation + bottom tabs
│   ├── Platform.kt               # expect: file picker / canUpload / readBytes
│   ├── api/                      # Ktor ApiClient, @Serializable models, TokenStore expect
│   ├── state/AppModel.kt         # single observable state holder (auth, data, player queue)
│   ├── state/ListenTogether.kt   # listening sessions: follows the server's timeline, sends controls
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
