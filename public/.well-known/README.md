# App link verification

These two files let shared playlist links (`https://<your-host>/playlists/<id>`)
open straight in the mobile app instead of a browser chooser. Both must be
served over HTTPS from the site root — this `public/` folder does that.

## Android — `assetlinks.json`

Replace `REPLACE_WITH_YOUR_SIGNING_CERT_SHA256` with the SHA-256 of the
certificate that signs your APK/AAB:

    keytool -list -v -keystore ~/.android/debug.keystore -alias androiddebugkey \
        -storepass android -keypass android | grep SHA256

(use your release keystore for release builds; you can list both
fingerprints). The manifest already sets `android:autoVerify="true"` and
`musicHost` in `mobile/gradle.properties` must match this site's host.

## iOS — `apple-app-site-association`

Replace `REPLACE_WITH_TEAMID` with your Apple Team ID (Xcode → Signing &
Capabilities). Add the Associated Domains capability to the iOS target with
`applinks:<your-host>`. Apple fetches this file through its CDN, so changes
can take a while to show up on devices.
