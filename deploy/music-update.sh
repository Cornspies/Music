#!/bin/sh
# Updates a Music server installed from a release bundle (README.md → Updates).
#
#   music-update                  install the latest release
#   music-update v0.1.3           install that release (also to reinstall one)
#   music-update <file.tar.gz>    install a bundle you downloaded yourself
#   music-update rollback         switch back to the release before
#   music-update status           show what's installed and what's available
#
# Run it as root. It stops the server, backs up the database, swaps in the new
# server and web app and starts it again. If the server doesn't come back, it
# goes back to the old release and the database from before. .env and data/
# are kept. With the nginx maintenance page set up (music-maintenance), users
# see "down for maintenance" in the meantime.
#
# Settings, as environment variables: MUSIC_DIR (default /opt/music),
# MUSIC_SERVICE (music-server), MUSIC_REPO (Cornspies/Music) and
# MUSIC_MAINTENANCE_DIR (/var/www/music-maintenance).
set -eu
umask 022

dir=${MUSIC_DIR:-/opt/music}
service=${MUSIC_SERVICE:-music-server}
repo=${MUSIC_REPO:-Cornspies/Music}
maintenance_dir=${MUSIC_MAINTENANCE_DIR:-/var/www/music-maintenance}
export MUSIC_MAINTENANCE_DIR="$maintenance_dir"
keep_backups=5

usage() {
	sed -n '4,8p' "$0" | sed 's/^# \{0,1\}//' >&2
	exit 2
}

die() {
	printf 'music-update: %s\n' "$*" >&2
	exit 1
}

need_root() {
	[ "$(id -u)" -eq 0 ] || die "run it as root (sudo music-update ...)."
	for cmd in curl tar sha256sum systemctl; do
		command -v "$cmd" >/dev/null 2>&1 || die "$cmd is missing."
	done
	[ -x "$dir/music-server" ] ||
		die "there's no server in $dir. Install it first (README.md → Run as a service), or set MUSIC_DIR."
	systemctl cat "$service" >/dev/null 2>&1 ||
		die "there's no $service service. Set MUSIC_SERVICE if yours has another name."
}

work=
locked=0
cleanup() {
	if [ -n "$work" ]; then rm -rf "$work"; fi
	if [ "$locked" = 1 ]; then rmdir "$dir/.update-lock" 2>/dev/null || true; fi
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM

lock() {
	mkdir "$dir/.update-lock" 2>/dev/null ||
		die "another update is running. If none is, remove $dir/.update-lock."
	locked=1
}

# A value from the server's .env, without quotes; empty when it isn't set.
env_value() {
	[ -f "$dir/.env" ] || return 0
	sed -n "s/^[[:space:]]*$1[[:space:]]*=[[:space:]]*//p" "$dir/.env" | tail -n 1 |
		sed -e 's/[[:space:]]*$//' -e 's/^"\(.*\)"$/\1/' -e "s/^'\(.*\)'$/\1/"
}

data_dir() {
	d=$(env_value MUSIC_DATA_DIR)
	d=${d:-data}
	case $d in
	/*) echo "$d" ;;
	*) echo "$dir/$d" ;;
	esac
}

health_url() {
	port=$(env_value MUSIC_PORT)
	bind=$(env_value MUSIC_BIND)
	case $bind in
	'' | 0.0.0.0 | :: | '[::]') host=127.0.0.1 ;;
	\[*) host=$bind ;;
	*:*) host="[$bind]" ;;
	*) host=$bind ;;
	esac
	echo "http://$host:${port:-3001}/healthz"
}

# Waits up to two minutes for the server to answer, less when it keeps crashing.
wait_healthy() {
	url=$(health_url)
	restarts=$(systemctl show -p NRestarts --value "$service" 2>/dev/null || echo 0)
	i=0
	while [ "$i" -lt 60 ]; do
		if curl -fsS -m 2 -o /dev/null "$url" 2>/dev/null; then return 0; fi
		if systemctl is-failed --quiet "$service"; then return 1; fi
		now=$(systemctl show -p NRestarts --value "$service" 2>/dev/null || echo 0)
		if [ "$((${now:-0} - ${restarts:-0}))" -ge 3 ]; then return 1; fi
		sleep 2
		i=$((i + 1))
	done
	return 1
}

installed() {
	head -n 1 "$dir/RELEASE" 2>/dev/null || echo unknown
}

release_json() {
	if [ "$1" = latest ]; then path=latest; else path=tags/$1; fi
	curl -fsL -H 'Accept: application/vnd.github+json' "https://api.github.com/repos/$repo/releases/$path"
}

json_tag() {
	awk -F'"' '$2 == "tag_name" { print $4; exit }'
}

# The SHA-256 GitHub lists for one of the release's files.
asset_digest() {
	awk -F'"' -v want="$1" '
		$2 == "name" { name = $4 }
		$2 == "digest" && name == want { sub(/^sha256:/, "", $4); print $4; exit }'
}

maintenance_set=0
maintenance_on() {
	if ! command -v music-maintenance >/dev/null 2>&1 || [ ! -d "$maintenance_dir" ]; then return 0; fi
	if [ -f "$maintenance_dir/maintenance.json" ]; then
		echo "Maintenance is already on; leaving it on."
		return 0
	fi
	if music-maintenance on 10m "$1"; then maintenance_set=1; fi
}

maintenance_off() {
	if [ "$maintenance_set" = 1 ]; then
		music-maintenance off
		maintenance_set=0
	fi
}

# Copies the database while the server is stopped; prints the backup folder.
# Only the owner can read backups: they hold password hashes and the key that
# signs logins.
backup_db() {
	data=$(data_dir)
	mkdir -p "$dir/backups"
	chmod 700 "$dir/backups"
	b=$dir/backups/$(date +%Y%m%d-%H%M%S)-$(installed)
	mkdir -m 700 "$b"
	for f in music.db music.db-wal music.db-shm secret.txt; do
		if [ -f "$data/$f" ]; then cp -p "$data/$f" "$b/"; fi
	done
	echo "$b"
}

restore_db() {
	data=$(data_dir)
	rm -f "$data/music.db-wal" "$data/music.db-shm"
	for f in music.db music.db-wal music.db-shm; do
		if [ -f "$1/$f" ]; then cp -p "$1/$f" "$data/"; fi
	done
}

prune_backups() {
	ls -1d "$dir"/backups/*/ 2>/dev/null | sort -r | tail -n +$((keep_backups + 1)) |
		while read -r old; do rm -rf "$old"; done
}

# Swaps the installed release with the one in previous/. BACKUP, the database
# from before an update, goes along with the release it belongs to.
swap_previous() {
	mkdir "$dir/.swap"
	for f in music-server web RELEASE BACKUP; do
		if [ -e "$dir/$f" ]; then mv "$dir/$f" "$dir/.swap/"; fi
		if [ -e "$dir/previous/$f" ]; then mv "$dir/previous/$f" "$dir/"; fi
		if [ -e "$dir/.swap/$f" ]; then mv "$dir/.swap/$f" "$dir/previous/"; fi
	done
	rmdir "$dir/.swap"
}

# Replaces a file that may be running (this script) without writing into it.
put() {
	cp "$1" "$2.new.$$"
	chmod "$3" "$2.new.$$"
	mv -f "$2.new.$$" "$2"
}

# Keeps the maintenance page, music-maintenance and this script up to date.
refresh_helpers() {
	if [ -f "$maintenance_dir/index.html" ] && [ -f "$dir/nginx/maintenance/index.html" ]; then
		put "$dir/nginx/maintenance/index.html" "$maintenance_dir/index.html" 644
	fi
	m=$(command -v music-maintenance 2>/dev/null || true)
	if [ -n "$m" ] && [ -f "$dir/nginx/maintenance.sh" ]; then put "$dir/nginx/maintenance.sh" "$m" 755; fi
	self=$(command -v music-update 2>/dev/null || true)
	if [ -n "$self" ] && [ -f "$dir/music-update.sh" ]; then put "$dir/music-update.sh" "$self" 755; fi
	unit=/etc/systemd/system/$service.service
	if [ -f "$unit" ] && [ -f "$dir/music-server.service" ] && ! cmp -s "$unit" "$dir/music-server.service"; then
		echo "This release's systemd unit differs from $unit: compare them with"
		echo "  diff $unit $dir/music-server.service"
		echo "and if you haven't changed yours, copy it over and run systemctl daemon-reload."
	fi
}

install_release() {
	src=$1
	lock
	old=$(installed)
	work=$(mktemp -d "$dir/.update.XXXXXX")
	bundle=$work/bundle.tar.gz
	tag=

	if [ -f "$src" ]; then
		cp "$src" "$bundle"
		echo "Installing $(basename "$src"), not checked against GitHub."
	else
		case $(uname -m) in
		x86_64 | amd64) arch=amd64 ;;
		aarch64 | arm64) arch=arm64 ;;
		*) die "there's no release for $(uname -m)." ;;
		esac
		json=$(release_json "$src") || die "couldn't find release $src on github.com/$repo."
		tag=$(printf '%s\n' "$json" | json_tag)
		[ -n "$tag" ] || die "couldn't read the release from GitHub."
		if [ "$src" = latest ] && [ "$tag" = "$old" ]; then
			echo "Already on $tag, the latest release."
			return 0
		fi
		asset=Music-server-$tag-linux-$arch.tar.gz
		sum=$(printf '%s\n' "$json" | asset_digest "$asset")
		[ -n "$sum" ] || die "release $tag has no $asset."
		echo "Downloading $asset"
		curl -fL --progress-bar -o "$bundle" "https://github.com/$repo/releases/download/$tag/$asset" ||
			die "the download failed; nothing was changed."
		printf '%s  %s\n' "$sum" "$bundle" | sha256sum -c --quiet - >/dev/null 2>&1 ||
			die "$asset doesn't match its checksum on GitHub; nothing was changed."
	fi

	new=$work/new
	mkdir "$new"
	tar -xzf "$bundle" -C "$new" --strip-components=1 2>/dev/null || die "that isn't a release bundle."
	[ -x "$new/music-server" ] && [ -f "$new/web/index.html" ] ||
		die "the bundle has no music-server or web app; nothing was changed."
	if [ -z "$tag" ]; then tag=$(head -n 1 "$new/RELEASE" 2>/dev/null || true); fi
	if [ -z "$tag" ]; then
		n=$(basename "$src" .tar.gz)
		n=${n#Music-server-}
		tag=${n%-linux-*}
	fi

	owner=$(stat -c %u:%g "$dir/music-server")
	echo "Updating $old to $tag."
	maintenance_on "Updating to $tag."
	systemctl stop "$service"
	backup=$(backup_db)

	# The release before stays aside until the new one is running.
	if [ -d "$dir/previous" ]; then mv "$dir/previous" "$work/previous"; fi
	mkdir "$dir/previous"
	for f in music-server web RELEASE; do
		if [ -e "$dir/$f" ]; then mv "$dir/$f" "$dir/previous/"; fi
	done
	rm -f "$dir/BACKUP"
	echo "$backup" >"$dir/previous/BACKUP"
	mv "$new/music-server" "$new/web" "$dir/"
	echo "$tag" >"$dir/RELEASE"
	# The rest of the bundle, for reference; the server doesn't read these.
	# Replaced as a whole, so they always match the installed release.
	for f in .env.example LICENSE music-server.service music-update.sh nginx; do
		rm -rf "${dir:?}/$f"
		if [ -e "$new/$f" ]; then mv "$new/$f" "$dir/"; fi
	done
	for f in music-server web RELEASE previous backups nginx .env.example LICENSE music-server.service music-update.sh; do
		if [ -e "$dir/$f" ]; then chown -R "$owner" "$dir/$f"; fi
	done

	systemctl start "$service" || true
	if wait_healthy; then
		maintenance_off
		refresh_helpers
		prune_backups
		echo "Updated to $tag. The database from before is in $backup."
		if [ "$old" = unknown ]; then old="the release before"; fi
		echo "music-update rollback switches back to $old."
		return 0
	fi

	echo "The new server didn't start; going back to $old." >&2
	systemctl stop "$service" || true
	swap_previous
	rm -rf "$dir/previous" "$dir/BACKUP"
	if [ -d "$work/previous" ]; then mv "$work/previous" "$dir/previous"; fi
	restore_db "$backup"
	systemctl start "$service" || true
	if wait_healthy; then
		maintenance_off
		die "updating to $tag failed and $old is running again. See journalctl -u $service and $(data_dir)/logs."
	fi
	die "updating to $tag failed and $old didn't come back either; maintenance stays on. See journalctl -u $service."
}

rollback() {
	[ -x "$dir/previous/music-server" ] || die "there's no earlier release in $dir/previous."
	lock
	current=$(installed)
	back=$(head -n 1 "$dir/previous/RELEASE" 2>/dev/null || echo "the earlier release")
	maintenance_on "Going back to $back."
	systemctl stop "$service"
	swap_previous
	systemctl start "$service" || true
	if wait_healthy; then
		maintenance_off
		echo "Back on $back, with the current database. Run music-update rollback again to return to $current."
		if [ -f "$dir/BACKUP" ]; then
			echo "If $back has trouble with it, the database from before the update is in $(cat "$dir/BACKUP")."
		fi
		return 0
	fi
	die "$back didn't start; maintenance stays on. See journalctl -u $service."
}

status() {
	echo "Installed: $(installed)"
	latest=$(release_json latest 2>/dev/null | json_tag || true)
	if [ -z "$latest" ]; then latest="unknown, GitHub didn't answer"; fi
	echo "Latest:    $latest"
	if [ -x "$dir/previous/music-server" ]; then
		echo "Previous:  $(head -n 1 "$dir/previous/RELEASE" 2>/dev/null || echo unknown) (music-update rollback)"
	fi
	if curl -fsS -m 2 -o /dev/null "$(health_url)" 2>/dev/null; then
		echo "Server:    running"
	else
		echo "Server:    not answering at $(health_url)"
	fi
}

[ $# -le 1 ] || usage
case ${1:-latest} in
status) status ;;
rollback)
	need_root
	rollback
	;;
-* | help) usage ;;
*)
	need_root
	install_release "${1:-latest}"
	;;
esac
