#!/usr/bin/env bash
set -euo pipefail

require=0
if [[ "${1:-}" == "--require" ]]; then
  require=1
  shift
fi
[[ $# == 0 ]] || { echo "usage: $0 [--require]" >&2; exit 2; }
export GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off TMPDIR=/tmp
if [[ -d vendor && -z "${GOFLAGS:-}" ]]; then
  export GOFLAGS=-mod=vendor
fi

mapfile -t model_files < <(grep -RIl --include='*_test.go' '^func TestModel' internal 2>/dev/null | sort -u || true)
mapfile -t fuzz_files < <(grep -RIl --include='*_test.go' '^func Fuzz' internal 2>/dev/null | sort -u || true)

run_packages() {
  local regex="$1"; shift
  local -a files=("$@") packages=()
  local file package
  for file in "${files[@]}"; do
    package="./$(dirname "$file")"
    [[ " ${packages[*]} " == *" $package "* ]] || packages+=("$package")
  done
  if ((${#packages[@]})); then
    go test -count=1 -run "$regex" "${packages[@]}"
  fi
}

model_status=missing
fuzz_status=missing
if ((${#model_files[@]})); then
  run_packages '^TestModel' "${model_files[@]}"
  model_status=passed
fi
if ((${#fuzz_files[@]})); then
  fuzz_cache="$(mktemp -d)"
  trap 'rm -rf -- "$fuzz_cache"' EXIT
  for file in "${fuzz_files[@]}"; do
    package="./$(dirname "$file")"
    while read -r name; do
      [[ -n "$name" ]] || continue
      GOCACHE="$fuzz_cache" go test -run '^$' -fuzz "^${name}$" -fuzztime=1s -parallel=1 "$package"
    done < <(sed -nE 's/^func (Fuzz[A-Za-z0-9_]+)\(.*/\1/p' "$file")
  done
  fuzz_status=passed
fi
printf '{"schema_version":1,"model":"%s","fuzz":"%s"}\n' "$model_status" "$fuzz_status"
if ((require)) && [[ "$model_status" != passed || "$fuzz_status" != passed ]]; then
  echo "queued property/fuzz layers are not both present; refusing false acceptance" >&2
  exit 3
fi
