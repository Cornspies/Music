# Third-party sources

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
