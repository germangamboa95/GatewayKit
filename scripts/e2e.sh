#!/usr/bin/env bash
# Usage: scripts/e2e.sh <config>   e.g. scripts/e2e.sh variant-b.yaml
# Runs the e2e suite against one config from ./configs (design D11).
# E2E_RUN=<regex> limits the tests run; LOCK_WAIT=<seconds> waits for the lock.
set -euo pipefail
cd "$(dirname "$0")/.."
. scripts/lock.sh

CONFIG="${1:-}"
if [ -z "$CONFIG" ] || [ "$CONFIG" != "$(basename "$CONFIG")" ]; then
	echo "usage: scripts/e2e.sh <config file name in configs/>" >&2
	exit 2
fi
if [ ! -f "configs/$CONFIG" ]; then
	echo "e2e: configs/$CONFIG not found" >&2
	exit 2
fi
GATEWAY_PORT=8080
[ "$CONFIG" = "variant-b.yaml" ] && GATEWAY_PORT=9090
export CONFIG GATEWAY_PORT

STARTED=0
# shellcheck disable=SC2329
cleanup() {
	local rc=$?
	if [ "$STARTED" = 1 ]; then
		docker compose --profile test down --remove-orphans -t 5 >/dev/null 2>&1 || true
	fi
	gk_lock_release
	exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

gk_lock_acquire || exit 3

PORTS="-iTCP:8080 -iTCP:9090 -iTCP:3001-3006"
if command -v lsof >/dev/null 2>&1; then
	# shellcheck disable=SC2086
	if busy="$(lsof -nP $PORTS -sTCP:LISTEN 2>/dev/null)" && [ -n "$busy" ]; then
		echo "e2e: ports already in use, stop the other stack first:" >&2
		echo "$busy" >&2
		exit 3
	fi
else
	echo "e2e: lsof not found, skipping port check" >&2
fi

echo "== e2e $CONFIG (gateway port $GATEWAY_PORT)"
STARTED=1
docker compose up -d --build --force-recreate --quiet-pull

wait_http() {
	local url="$1" deadline=$((SECONDS + 60)) code
	while :; do
		code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "$url" || true)"
		[ "$2" = any ] && [ "$code" != 000 ] && return 0
		[ "$code" = "$2" ] && return 0
		if [ "$SECONDS" -ge "$deadline" ]; then
			echo "e2e: $url not ready (last status $code)" >&2
			return 1
		fi
		sleep 0.5
	done
}

ready=1
wait_http "http://127.0.0.1:$GATEWAY_PORT/health" 200 || ready=0
for p in 3001 3002 3003 3004 3005 3006; do
	[ "$ready" = 1 ] || break
	wait_http "http://127.0.0.1:$p/" any || ready=0
done
if [ "$ready" = 0 ]; then
	docker compose ps -a >&2
	docker compose logs --tail=50 gateway >&2
	exit 1
fi

set +e
if [ -n "${E2E_RUN:-}" ]; then
	docker compose run --build --rm --no-deps tests go test -v -count=1 -run "$E2E_RUN" ./...
else
	docker compose run --build --rm --no-deps tests
fi
rc=$?
set -e
if [ "$rc" != 0 ]; then
	echo "e2e: $CONFIG failed (exit $rc); gateway log tail:" >&2
	docker compose logs --tail=50 gateway >&2
fi
exit "$rc"
