#!/usr/bin/env bash
# Full CI (design D13): gofmt, dependency check, vet and unit tests in all
# three modules, then scripts/e2e.sh for each config in ./configs.
set -euo pipefail
cd "$(dirname "$0")/.."
. scripts/lock.sh

export GOWORK=off GOFLAGS=
unset GATEWAY_URL
MODULES=". mock-upstream tests"
CONFIGS="gateway.yaml variant-a.yaml variant-b.yaml variant-c.yaml"

step() { echo "== $*"; }
die() {
	echo "FAIL: $*" >&2
	exit 1
}

step "gofmt"
unformatted="$(find . \( -path ./.git -o -path ./.claude \) -prune -o -name '*.go' -type f -print0 | xargs -0 gofmt -l)"
[ -z "$unformatted" ] || die "gofmt needed:"$'\n'"$unformatted"

# The gateway builds from itself and gopkg.in/yaml.v3 only. The mocks and the
# e2e tests build from themselves and the standard library only.
step "dependency check"
bad=""
for m in $MODULES; do
	[ -f "$m/go.mod" ] || die "$m/go.mod missing"
	name="$(awk '$1 == "module" { print $2 }' "$m/go.mod")"
	want="$name"
	[ "$m" != . ] || want="$name"$'\n'"gopkg.in/yaml.v3"
	if got="$(cd "$m" && go list -deps -test -f '{{with .Module}}{{.Path}}{{end}}' ./... 2>&1)"; then
		got="$(printf '%s\n' "$got" | sed '/^$/d' | LC_ALL=C sort -u)"
	fi
	if [ "$got" != "$want" ]; then
		echo "deps: module $name ($m) builds from:"$'\n'"$got"$'\n'"want:"$'\n'"$want" >&2
		bad="$bad $name"
	fi
done
[ -z "$bad" ] || die "dependency check failed for:$bad"

for m in $MODULES; do
	step "go vet, go test ($m)"
	(cd "$m" && go vet ./... && go test -count=1 ./...) || die "unit gates failed in $m"
done

if [ ! -d configs ] || [ -z "$(ls -A configs 2>/dev/null)" ]; then
	echo "SKIP e2e: configs/ does not exist yet"
	echo "ci: OK (e2e skipped)"
	exit 0
fi

# Dead-target check (design D11): on variant-c.yaml, stop products-b from the host,
# expect 502s, then all /c/health traffic on products-a within 3s, then recovery.
DT_STARTED=0
DT_BASE="http://127.0.0.1:8080"
DT_TMP=""

ci_cleanup() {
	dt_down
	[ -z "$DT_TMP" ] || rm -rf "$DT_TMP"
	gk_lock_release
}

dt_down() {
	if [ "$DT_STARTED" = 1 ]; then
		docker compose --profile test down --remove-orphans -t 5 >/dev/null 2>&1 || true
		DT_STARTED=0
	fi
}

now_ms() {
	if [ -n "${EPOCHREALTIME:-}" ]; then
		local t="${EPOCHREALTIME/[.,]/}"
		echo $((10#$t / 1000))
	elif command -v perl >/dev/null 2>&1; then
		perl -MTime::HiRes=time -e 'printf "%d\n", time * 1000'
	else
		echo $(($(date +%s) * 1000))
	fi
}

# dt_hit <path> prints "<status> <X-Mock-Service or ->"; the body is left in $DT_TMP/body.
dt_hit() {
	local code svc
	: >"$DT_TMP/hdr"
	: >"$DT_TMP/body"
	code="$(curl -s --max-time 3 -D "$DT_TMP/hdr" -o "$DT_TMP/body" -w '%{http_code}' "$DT_BASE$1")" || code=000
	svc="$(tr -d '\r' <"$DT_TMP/hdr" | awk 'tolower($1) == "x-mock-service:" { print $2 }')"
	echo "$code ${svc:--}"
}

dt_wait() {
	local url="$1" want="$2" deadline=$((SECONDS + 60)) code
	while :; do
		code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "$url" || true)"
		[ "$want" = any ] && [ "$code" != 000 ] && return 0
		[ "$code" = "$want" ] && return 0
		[ "$SECONDS" -lt "$deadline" ] || {
			echo "dead-target: $url not ready (last status $code)" >&2
			return 1
		}
		sleep 0.5
	done
}

# dt_until <ms> <cmd...> runs cmd every 50ms until it succeeds or ms pass.
dt_until() {
	local end=$(($(now_ms) + $1))
	shift
	while ! "$@"; do
		[ "$(now_ms)" -lt "$end" ] || return 1
		sleep 0.05
	done
}

dt_both_serve() {
	local x y
	x="$(dt_hit /c/health/dt)"
	y="$(dt_hit /c/health/dt)"
	[ "${x%% *}" = 200 ] && [ "${y%% *}" = 200 ] && [ "${x#* }" != "${y#* }" ]
}

dt_saw_502() {
	local out
	out="$(dt_hit /c/health/dt)"
	[ "$out" = "502 -" ] && grep -q '"bad_gateway"' "$DT_TMP/body"
}

dt_only_a() {
	local i
	for i in 1 2 3 4 5 6; do
		[ "$(dt_hit /c/health/dt)" = "200 products-a" ] || return 1
	done
}

dt_saw_b() {
	[ "$(dt_hit /c/health/dt)" = "200 products-b" ]
}

dead_target_check() {
	local t0 took busy i out seen=""
	export CONFIG=variant-c.yaml GATEWAY_PORT=8080
	step "dead-target check ($CONFIG)"
	if command -v lsof >/dev/null 2>&1 &&
		busy="$(lsof -nP -iTCP:8080 -iTCP:9090 -iTCP:3001-3006 -sTCP:LISTEN 2>/dev/null)" && [ -n "$busy" ]; then
		echo "dead-target: ports already in use:"$'\n'"$busy" >&2
		return 1
	fi
	DT_TMP="$(mktemp -d)"
	DT_STARTED=1
	docker compose up -d --build --force-recreate --quiet-pull || return 1
	dt_wait "$DT_BASE/health" 200 || return 1
	for i in 3003 3004; do
		dt_wait "http://127.0.0.1:$i/" any || return 1
	done
	dt_until 5000 dt_both_serve || {
		echo "dead-target: /c/health does not rotate over both targets" >&2
		return 1
	}

	docker compose stop -t 0 products-b || return 1
	t0="$(now_ms)"
	dt_until 1500 dt_saw_502 || {
		echo "dead-target: no 502 bad_gateway after stopping products-b" >&2
		return 1
	}
	echo "dead-target: 502 bad_gateway after $(($(now_ms) - t0))ms"
	dt_until $((3000 - ($(now_ms) - t0))) dt_only_a || {
		echo "dead-target: /c/health traffic not all on products-a within 3s" >&2
		return 1
	}
	took=$(($(now_ms) - t0))
	[ "$took" -le 3000 ] || {
		echo "dead-target: products-b left rotation after ${took}ms, want 3000ms or less" >&2
		return 1
	}
	echo "dead-target: all traffic on products-a after ${took}ms"
	for i in 1 2 3 4 5 6 7 8 9 10; do
		out="$(dt_hit /c/health/dt)"
		[ "$out" = "200 products-a" ] || {
			echo "dead-target: request $i after removal got \"$out\"" >&2
			return 1
		}
	done

	docker compose start products-b || return 1
	dt_wait "http://127.0.0.1:3004/" any || return 1
	t0="$(now_ms)"
	dt_until 5000 dt_saw_b || {
		echo "dead-target: products-b not back in rotation within 5s of start" >&2
		return 1
	}
	echo "dead-target: products-b back after $(($(now_ms) - t0))ms"
	for i in 1 2 3 4; do
		out="$(dt_hit /c/health/dt)"
		[ "${out%% *}" = 200 ] || {
			echo "dead-target: request after recovery got \"$out\"" >&2
			return 1
		}
		seen="$seen ${out#* }"
	done
	case "$seen" in
	*products-a*products-b* | *products-b*products-a*) ;;
	*)
		echo "dead-target: 4 requests after recovery went to:$seen" >&2
		return 1
		;;
	esac
	echo "dead-target: OK"
}

trap ci_cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
gk_lock_acquire || exit 3

failed=""
for c in $CONFIGS; do
	if [ ! -f "configs/$c" ]; then
		echo "FAIL: configs/$c missing" >&2
		failed="$failed $c"
		continue
	fi
	scripts/e2e.sh "$c" || failed="$failed $c"
done
if [ -f configs/variant-c.yaml ]; then
	if ! dead_target_check; then
		docker compose logs --tail=50 gateway >&2 || true
		failed="$failed dead-target"
	fi
	dt_down
fi
[ -z "$failed" ] || die "e2e failed for:$failed"
echo "ci: OK"
