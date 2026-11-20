# Running a Music server

What comes after the [quick install](../README.md#quick-install): running the
server as a service, updating it, putting it behind HTTPS, and telling people
when it is down. Every setting is explained in [.env.example](../.env.example).

## After the quick install

- **It stays in its folder.** The server reads the `.env` next to it and keeps
  everything it stores in `data/` beside it, wherever it is started from.
- **Other devices** use `http://<this machine's address>:3001`, which the apps
  ask for when you sign in. To reach it over the internet, put it behind a
  reverse proxy with HTTPS (see [Deploy it](#deploy-it)).
- **ffmpeg** is optional. Install it for format conversion for iOS, loudness
  analysis and waveforms.

## Run as a service (Linux)

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

## Updates

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

When `.env` sets `MUSIC_APK_DIR` and `MUSIC_UPDATE_APPS`, it also downloads
the release's app builds into that folder (checked the same way), so the
server offers them under "Other platforms" and the apps update to them.
`MUSIC_UPDATE_APPS` names the kinds to fetch: `android`, `appimage`, `deb`,
`rpm` and `windows`, separated by commas, or `all`. Without it none are
fetched. Older builds of the same kind there are removed; one of the same name
but different contents, such as your own build, is kept. Running
`music-update` when you're already on the latest release fetches what's
missing. A signed desktop build's `.sig` file comes with it; the desktop app
needs it to update itself (see [Desktop](desktop.md#updates)).

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

Without the service: stop the server, back up `data/`, delete `web/` and
extract the new release into the same folder (on Linux, `tar -xzf <file>
--strip-components=1 -C <folder>`). `.env` and `data/` aren't part of a
release, so they are kept.

## Monitoring

Set `MUSIC_METRICS_ADDR` (for example `127.0.0.1:9464`) to serve Prometheus
metrics at `/metrics` on a separate port: requests by route and status,
latency, errors, plays, library size, background queues, and Go and process
stats. Set `MUSIC_METRICS_TOKEN` as well when Prometheus scrapes from another
machine. `/healthz` on the main port answers `{"status":"ok"}` for uptime
checks.

## Deploy it

Put the server behind a reverse proxy that provides HTTPS (Caddy, nginx or
Traefik all work), and send everything, `/api` included, to the server's
port. For nginx, [deploy/nginx/music.conf](../deploy/nginx/music.conf) is a
complete site with a maintenance switch (see [Maintenance](#maintenance)).

To build and serve everything from source, one process serves both:

```sh
bun run build                       # writes the web app to out/
cd backend/music-server
go build -o music-server .
MUSIC_WEB_DIR=../../out ./music-server
```

The data folder (`MUSIC_DATA_DIR`) is the only thing to back up. A first
session secret is generated into it; set `MUSIC_JWT_SECRET` if you rebuild
the folder and want existing sessions to survive.

If you lock yourself out, `music-server -reset-password <username>` prints a
one-time reset link for that account.

## Maintenance

With nginx in front, you can tell people when Music will be back while you
work on the server. The files are in [deploy/nginx](../deploy/nginx), and in the
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
