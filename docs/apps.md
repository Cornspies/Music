# The apps and your own builds

- [Desktop](../desktop/README.md): installers for Windows and Linux.
- [Android and iOS](../mobile/README.md): build instructions, signing,
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

The privacy page says no policy has been published until you set
`MUSIC_OPERATOR` and `MUSIC_CONTACT` in `.env`.
