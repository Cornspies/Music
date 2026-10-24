#!/bin/sh
# Turns the nginx maintenance page on and off (music.conf next to this).
#
#   maintenance.sh on 30m                     back in 30 minutes
#   maintenance.sh on 2h "Moving to a new disk."
#   maintenance.sh on 1h30m
#   maintenance.sh off
#   maintenance.sh status
#
# Run it as a user that can write to the folder (usually with sudo). nginx
# picks the change up at once; no reload is needed. Set MUSIC_MAINTENANCE_DIR
# when the page lives somewhere other than /var/www/music-maintenance.
set -eu

dir=${MUSIC_MAINTENANCE_DIR:-/var/www/music-maintenance}
flag=$dir/maintenance.json

usage() {
	sed -n '4,8p' "$0" | sed 's/^# \{0,1\}//' >&2
	exit 2
}

# 1h30m, 90m, 2h or 45 (minutes) -> seconds.
seconds() {
	case $1 in
	'' | *[!0-9hm]* | *h*h* | *m*m* | *m?* | h* | m*) return 1 ;;
	esac
	rest=$1 total=0
	case $rest in *h*)
		total=$((${rest%%h*} * 3600))
		rest=${rest#*h}
		;;
	esac
	rest=${rest%m}
	[ -n "$rest" ] && total=$((total + rest * 60))
	[ "$total" -gt 0 ] || return 1
	echo "$total"
}

# Escapes a string for a JSON value.
json() {
	printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' -e 's/\t/\\t/g' | awk 'NR > 1 { printf "\\n" } { printf "%s", $0 }'
}

case ${1:-} in
on)
	[ $# -ge 2 ] && [ $# -le 3 ] || usage
	secs=$(seconds "$2") || { echo "Can't read \"$2\" as a duration: use 30m, 2h or 1h30m." >&2; exit 2; }
	until=$(date -u -d "@$(($(date +%s) + secs))" +%Y-%m-%dT%H:%M:%SZ)
	message=${3:-}
	[ -d "$dir" ] || { echo "$dir doesn't exist: copy the maintenance/ folder there first." >&2; exit 1; }
	tmp=$(mktemp "$dir/.maintenance.XXXXXX")
	printf '{"error":"Music is down for maintenance.","maintenance":true,"until":"%s","message":"%s"}\n' \
		"$until" "$(json "$message")" >"$tmp"
	chmod 644 "$tmp"
	mv -f "$tmp" "$flag"
	echo "Maintenance on, back around $(date -d "$until" '+%H:%M %Z')."
	;;
off)
	[ $# -eq 1 ] || usage
	rm -f "$flag"
	echo "Maintenance off."
	;;
status)
	if [ -f "$flag" ]; then cat "$flag"; else echo "Maintenance is off."; fi
	;;
*)
	usage
	;;
esac
