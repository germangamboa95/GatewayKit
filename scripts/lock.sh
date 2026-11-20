# Compose lock shared by e2e.sh and ci.sh (design D13). Source, do not run.
GK_LOCK="${TMPDIR:-/tmp}"
GK_LOCK="${GK_LOCK%/}/gatewaykit-compose.lock"
GK_LOCK_MINE=0

# gk_lock_acquire takes the lock, or reuses it when a parent (ci.sh) holds it.
# LOCK_WAIT=<seconds> waits for a busy lock instead of failing at once.
gk_lock_acquire() {
	if [ -n "${GK_LOCK_HELD:-}" ] && [ "$(cat "$GK_LOCK/pid" 2>/dev/null)" = "$GK_LOCK_HELD" ]; then
		return 0
	fi
	local waited=0 owner
	while ! mkdir "$GK_LOCK" 2>/dev/null; do
		owner="$(cat "$GK_LOCK/pid" 2>/dev/null || true)"
		if [ -n "$owner" ] && ! kill -0 "$owner" 2>/dev/null; then
			echo "lock: removing stale $GK_LOCK (pid $owner not running)" >&2
			rm -rf "$GK_LOCK"
			continue
		fi
		if [ "$waited" -ge "${LOCK_WAIT:-0}" ]; then
			echo "lock: $GK_LOCK is held by pid ${owner:-unknown}; another Compose stack is running" >&2
			return 1
		fi
		sleep 2
		waited=$((waited + 2))
	done
	echo "$$" >"$GK_LOCK/pid"
	GK_LOCK_MINE=1
	export GK_LOCK_HELD="$$"
}

gk_lock_release() {
	if [ "$GK_LOCK_MINE" = 1 ]; then
		rm -rf "$GK_LOCK"
		GK_LOCK_MINE=0
	fi
}
