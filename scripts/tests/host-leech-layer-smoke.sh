#!/usr/bin/env bash
set -euo pipefail

case "${1:-}" in
  ""|--require) ;;
  *) echo "usage: $0 [--require]" >&2; exit 2 ;;
esac
[[ $# -le 1 ]] || { echo "usage: $0 [--require]" >&2; exit 2; }

export GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off TMPDIR=/tmp
if [[ -d vendor && -z "${GOFLAGS:-}" ]]; then
  export GOFLAGS=-mod=vendor
fi

go test -count=1 -run '^TestModelControllerGeneratedSequences$' ./internal/slicecontroller

fuzz_cache="$(mktemp -d /tmp/terminal-redeemer-fuzz.XXXXXX)"
trap 'rm -rf -- "$fuzz_cache"' EXIT
fuzz_targets=(
  './internal/sliceenv:FuzzProcessEnvironmentMetadata'
  './internal/sourceinventory:FuzzSourceInventoryStore'
  './internal/zellijlive:FuzzProcessArgvEnvironmentMetadata'
  './internal/slicelaunch:FuzzRoutedIntentJournal'
  './internal/slicelaunch:FuzzRoutedIntentValidation'
  './internal/slicetransport:FuzzRPCResponse'
  './internal/sliceprotocol:FuzzInventoryEnvelope'
  './internal/sliceprotocol:FuzzCanonicalHashing'
  './internal/sliceprotocol:FuzzWorkspaceNormalization'
  './internal/sliceprotocol:FuzzDuplicateAndTruncatedJSON'
  './internal/slicerpc:FuzzHostTokenJournal'
  './internal/slicerpc:FuzzTokenRecordValidation'
  './internal/slicerpc:FuzzRPCRequest'
  './internal/slicerpc:FuzzRPCPayload'
  './internal/slicecontroller:FuzzControlRequestAndResponse'
  './internal/slicecontroller:FuzzControllerStateStore'
  './internal/slicecontroller:FuzzProjectionArgv'
)

# Go owns discovery: compare `go test -list` output with the reviewed explicit
# campaign list so a newly added target cannot silently miss the mandatory run.
declared="$fuzz_cache/declared"
discovered="$fuzz_cache/discovered"
printf '%s\n' "${fuzz_targets[@]}" | sort -u >"$declared"
for package in $(printf '%s\n' "${fuzz_targets[@]%%:*}" | sort -u); do
  while IFS= read -r target; do
    if [[ $target == Fuzz* ]]; then
      printf '%s:%s\n' "$package" "$target"
    fi
  done < <(GOCACHE="$fuzz_cache" go test -list '^Fuzz' "$package")
done | sort -u >"$discovered"
if ! cmp -s "$declared" "$discovered"; then
  echo "explicit fuzz target list differs from native go test discovery:" >&2
  diff -u "$declared" "$discovered" >&2 || true
  exit 1
fi
[[ $(wc -l <"$discovered") -eq 17 ]] || { echo "expected exactly 17 fuzz targets" >&2; exit 1; }

for entry in "${fuzz_targets[@]}"; do
  package="${entry%%:*}"
  target="${entry#*:}"
  GOCACHE="$fuzz_cache" go test -run '^$' -fuzz "^${target}$" -fuzztime=100x -parallel=1 "$package"
done
