#!/usr/bin/env bash
# Full CI (design D13): gofmt, standard-library check, vet and unit tests in
# all three modules, then scripts/e2e.sh for each config in ./configs.
set -euo pipefail
cd "$(dirname "$0")/.."
. scripts/lock.sh

export GOPROXY=off GOWORK=off GOFLAGS=
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

step "standard-library check"
bad=""
for m in $MODULES; do
	[ -f "$m/go.mod" ] || die "$m/go.mod missing"
	name="$(awk '$1 == "module" { print $2 }' "$m/go.mod")"
	if grep -nE '^[[:space:]]*require([[:space:](]|$)' "$m/go.mod" >&2; then
		echo "stdlib: module $name ($m/go.mod) has require lines" >&2
		bad="$bad $name"
		continue
	fi
	if ! all="$(cd "$m" && go list -m all 2>&1)" || [ "$all" != "$name" ]; then
		echo "stdlib: module $name ($m) go list -m all printed:"$'\n'"$all" >&2
		bad="$bad $name"
	fi
done
[ -z "$bad" ] || die "standard-library check failed for:$bad"

for m in $MODULES; do
	step "go vet, go test ($m)"
	(cd "$m" && go vet ./... && go test -count=1 ./...) || die "unit gates failed in $m"
done

if [ ! -d configs ] || [ -z "$(ls -A configs 2>/dev/null)" ]; then
	echo "SKIP e2e: configs/ does not exist yet"
	echo "ci: OK (e2e skipped)"
	exit 0
fi

trap gk_lock_release EXIT
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
[ -z "$failed" ] || die "e2e failed for:$failed"
echo "ci: OK"
