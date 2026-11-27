#!/usr/bin/env bash
# delivery-outside-timer.sh [data-dir]
#
# Watches datawatch from outside it. datawatch writes
# <data dir>/delivery/heartbeat.json after each delivery check. This script
# alerts when that file is missing or unreadable, is older than two check
# intervals plus 60 seconds, or says datawatch cannot tell you anything.
# A fresh "ok" or "red" is not an alert: datawatch can tell you itself.
#
# It reads that one file and writes nothing in the data folder. It needs bash
# and coreutils, and nothing from datawatch.
#
# The data folder is the argument, else $DATAWATCH_DATA_DIR, else
# ~/.datawatch.
#
# A delivered alert is not repeated for DELIVERY_TIMER_REPEAT_SECONDS (default
# 21600, six hours) while the reason stays the same. The note of the last one
# is kept in DELIVERY_TIMER_STATE_DIR (default
# ~/.local/state/datawatch-delivery-timer). An alert that was not delivered is
# tried again at every run.
#
# Exit: 0 nothing to report, or the alert was delivered; 1 there is an alert
# and it was not delivered; 2 wrong use.

set -u

# ===========================================================================
# MESSAGE ROUTE: THIS FUNCTION IS WHERE IT PLUGS IN.
#
# No route is built in. As shipped it prints what it would report and fails, so
# the timer unit shows as failed instead of looking as if someone was told.
#
# A route gets the whole message as $1 and must return 0 only when the message
# was accepted for delivery. It must not depend on datawatch, which is the
# thing that may be dead, and it must give up on its own after a short time.
# ===========================================================================
delivery_timer_send_alert() {
	printf 'delivery outside timer: no message route is set up. Not sent:\n%s\n' "$1" >&2
	return 1
}

# delivery_timer_check <heartbeat file> <now> sets TIMER_REASON, empty when
# there is nothing to report, and TIMER_SAYS, one sentence about it.
delivery_timer_check() {
	local file=$1 now=$2 body unix interval state limit age
	local unix_re='"unix":([0-9]{1,12})[,}]'
	local interval_re='"interval_seconds":([0-9]{1,5})[,}]'
	local state_re='"state":"(ok|red|cannot_tell)"'

	TIMER_REASON=heartbeat_missing
	TIMER_SAYS="the heartbeat file is not there"
	[ -e "$file" ] || return 0

	TIMER_REASON=heartbeat_unreadable
	TIMER_SAYS="the heartbeat file cannot be read"
	# Only a plain file: reading a pipe would hang.
	[ -f "$file" ] && [ -r "$file" ] || return 0
	body=$(head -c 4096 -- "$file" 2>/dev/null | LC_ALL=C tr -cd '[:graph:]') || return 0
	[[ $body == \{*\} ]] || return 0
	[[ $body =~ $unix_re ]] || return 0
	unix=$((10#${BASH_REMATCH[1]}))
	[[ $body =~ $interval_re ]] || return 0
	interval=$((10#${BASH_REMATCH[1]}))
	# An interval over a day would keep a dead heartbeat fresh.
	[ "$interval" -ge 1 ] && [ "$interval" -le 86400 ] || return 0
	[[ $body =~ $state_re ]] || return 0
	state=${BASH_REMATCH[1]}

	limit=$((2 * interval + 60))
	age=$((now - unix))
	if [ "$age" -gt "$limit" ]; then
		TIMER_REASON=heartbeat_stale
		TIMER_SAYS="the last completed check is ${age} seconds old; the limit is ${limit}"
	elif [ "$((-age))" -gt "$limit" ]; then
		TIMER_REASON=heartbeat_in_future
		TIMER_SAYS="the heartbeat is dated $((-age)) seconds ahead of this clock, so its age cannot be judged"
	elif [ "$state" = cannot_tell ]; then
		TIMER_REASON=cannot_tell
		TIMER_SAYS="datawatch says it cannot tell you what is waiting"
	else
		TIMER_REASON=''
		TIMER_SAYS=''
	fi
	return 0
}

delivery_timer_main() {
	if [ "$#" -gt 1 ]; then
		echo "usage: delivery-outside-timer.sh [data-dir]" >&2
		return 2
	fi
	local data=${1:-${DATAWATCH_DATA_DIR:-${HOME:-}/.datawatch}}
	local repeat=${DELIVERY_TIMER_REPEAT_SECONDS:-21600}
	local state_dir=${DELIVERY_TIMER_STATE_DIR:-${XDG_STATE_HOME:-${HOME:-}/.local/state}/datawatch-delivery-timer}
	local note=$state_dir/last-sent now last_reason='' last_at='' message
	[[ $repeat =~ ^[0-9]{1,9}$ ]] || repeat=21600
	now=$(date +%s)

	delivery_timer_check "$data/delivery/heartbeat.json" "$now"
	if [ -z "$TIMER_REASON" ]; then
		rm -f -- "$note" 2>/dev/null
		return 0
	fi

	if [ -f "$note" ] && [ -r "$note" ]; then
		read -r last_reason last_at <"$note" || true
	fi
	if [ "$last_reason" = "$TIMER_REASON" ] && [[ $last_at =~ ^[0-9]{1,12}$ ]] &&
		[ "$((now - last_at))" -ge 0 ] && [ "$((now - last_at))" -lt "$repeat" ]; then
		echo "delivery outside timer: $TIMER_REASON, already reported $((now - last_at)) seconds ago" >&2
		return 0
	fi

	message="[$(uname -n)] datawatch delivery check: $TIMER_SAYS (reason: $TIMER_REASON). datawatch may be down or unable to tell you anything. Check it on that host."
	delivery_timer_send_alert "$message" || return 1
	# Without the note the next run reports again, which is the safe side.
	if mkdir -p -- "$state_dir" 2>/dev/null; then
		{ printf '%s %s\n' "$TIMER_REASON" "$now" >"$note"; } 2>/dev/null || true
	fi
	return 0
}

# Run only when executed; a test sources this file and replaces the route.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	delivery_timer_main "$@"
fi
