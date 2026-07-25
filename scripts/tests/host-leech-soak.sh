#!/usr/bin/env bash
set -euo pipefail

iterations=2500
case "${1:-}" in
  "") ;;
  --iterations)
    [[ $# == 2 && "$2" =~ ^[0-9]+$ ]] || { echo "usage: $0 [--iterations 2000..50000]" >&2; exit 2; }
    iterations="$2"
    ;;
  *) echo "usage: $0 [--iterations 2000..50000]" >&2; exit 2 ;;
esac
(( iterations >= 2000 && iterations <= 50000 )) || { echo "iterations must be within [2000,50000]" >&2; exit 2; }

export GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off
if [[ -d vendor && -z "${GOFLAGS:-}" ]]; then
  export GOFLAGS=-mod=vendor
fi
# Resolve optional existing caches before HOME changes. No cache is accepted
# unless it is an absolute, direct, current-user-owned directory.
resolved_modcache="${HOST_LEECH_GOMODCACHE:-$(go env GOMODCACHE)}"
resolved_gocache="${HOST_LEECH_GOCACHE:-$(go env GOCACHE)}"
for item in "GOMODCACHE:$resolved_modcache" "GOCACHE:$resolved_gocache"; do
  name="${item%%:*}"; value="${item#*:}"
  [[ "$value" == /* && -d "$value" && ! -L "$value" && -O "$value" ]] || {
    echo "unsafe $name path: $value" >&2
    exit 2
  }
  printf -v "$name" '%s' "$value"
  export "$name"
done
unset NIRI_SOCKET WAYLAND_DISPLAY WAYLAND_SOCKET SSH_AUTH_SOCK SSH_AGENT_PID
unset ZELLIJ ZELLIJ_SESSION_NAME ZELLIJ_SOCKET_DIR ZELLIJ_CACHE_DIR
unset XDG_CONFIG_HOME XDG_STATE_HOME XDG_RUNTIME_DIR XDG_CACHE_HOME

private="$(mktemp -d /tmp/terminal-redeemer-soak.XXXXXX)"
trap 'rm -rf -- "$private"' EXIT
chmod 700 "$private"
summary="$private/summary.json"
log="$private/go-test.log"
export HOME="$private/home"
mkdir -m 700 "$HOME"
export TMPDIR=/tmp
export TERMINAL_REDEEMER_SOAK_ITERATIONS="$iterations"
export TERMINAL_REDEEMER_SOAK_SUMMARY="$summary"

if ! go test -count=1 -run '^TestBoundedHostLeechSoak$' -timeout=30m ./internal/hostleechsoak >"$log" 2>&1; then
  cat "$log" >&2
  exit 1
fi
[[ -s "$summary" ]] || { echo "soak did not produce summary" >&2; exit 1; }
python3 - "$summary" <<'PY'
import json, pathlib, sys
def _strict(pairs):
    out = {}
    for key, val in pairs:
        if key in out:
            raise ValueError(f"duplicate JSON key: {key}")
        out[key] = val
    return out
path = pathlib.Path(sys.argv[1])
raw = path.read_bytes()
value = json.loads(raw, object_pairs_hook=_strict)
if value.get("schema_version") != 1 or value.get("iterations", 0) < 2000:
    raise SystemExit("invalid soak summary identity")
if value.get("secrets_included") is not False:
    raise SystemExit("soak summary secret flag is not false")
caps = value.get("caps", {})
for name, metric in caps.items():
    if set(metric) != {"observed", "limit"} or metric["observed"] > metric["limit"]:
        raise SystemExit(f"invalid/exceeded cap {name}")
required_caps = {
    "sources", "projections", "session_drops", "selected_workspaces", "pickups",
    "successor_gates", "pending_cleanups", "lineage", "launch_handoffs",
    "handoff_tombstones", "spatial_records", "audit", "undo",
    "retired_epoch_tombstones", "routed_intent_files", "host_token_journal_records",
    "controller_state_bytes", "projection_argv_entries", "projection_argv_entry_bytes",
    "projection_argv_total_bytes", "host_session_creates", "host_kitty_starts",
    "host_placements", "host_source_commits", "routed_projection_launches",
    "routed_transport_attempts",
}
if not required_caps.issubset(caps):
    raise SystemExit(f"missing soak caps: {sorted(required_caps - set(caps))}")
effects = value.get("effects", {})
for name in ("host_session_create", "host_kitty_start", "host_placement", "host_source_commit", "routed_local_projection_launch"):
    if effects.get(name) != 1:
        raise SystemExit(f"invalid routed host effect cardinality {name}")
if effects.get("routed_transport_attempts") != 2:
    raise SystemExit("routed restart/replay did not make exactly two transport attempts")
if value.get("effects", {}).get("duplicate_active_projection", 0) != 0:
    raise SystemExit("duplicate projection effect observed")
if value.get("effects", {}).get("host_target_spatial", 0) != 0:
    raise SystemExit("host-target spatial effect observed")
if any(value.get("resources", {}).get(name, 0) != 0 for name in (
    "child_processes_remaining", "prepared_namespaces_remaining", "temporary_caches_remaining"
)):
    raise SystemExit("resource leak in soak summary")
sys.stdout.buffer.write(raw)
PY
