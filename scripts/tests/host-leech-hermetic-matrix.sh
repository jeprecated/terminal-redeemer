#!/usr/bin/env bash
set -euo pipefail

# This matrix is intentionally hermetic: all compositor, transport, process,
# clock, and filesystem inputs are fixtures/fakes or temporary state.
export GOTOOLCHAIN=local
if [[ -d vendor && -z "${GOFLAGS:-}" ]]; then
  export GOFLAGS=-mod=vendor
fi
matrix_home="$(mktemp -d)"
trap 'rm -rf -- "$matrix_home"' EXIT
export HOME="$matrix_home"

# Cache reuse is opt-in rather than inherited. Explicit paths must be absolute,
# direct, current-user-owned directories; otherwise the hermetic run refuses.
use_explicit_cache() {
  local value="$1" variable="$2"
  [[ "$value" == /* && -d "$value" && ! -L "$value" && -O "$value" ]] || {
    printf 'unsafe explicit %s path: %s\n' "$variable" "$value" >&2
    exit 2
  }
  printf -v "$variable" '%s' "$value"
  export "$variable"
}
unset GOMODCACHE GOCACHE
if [[ -n "${HOST_LEECH_GOMODCACHE:-}" ]]; then
  use_explicit_cache "$HOST_LEECH_GOMODCACHE" GOMODCACHE
fi
if [[ -n "${HOST_LEECH_GOCACHE:-}" ]]; then
  use_explicit_cache "$HOST_LEECH_GOCACHE" GOCACHE
fi
export GOPROXY=off
export GOSUMDB=off
unset XDG_CACHE_HOME XDG_CONFIG_HOME XDG_STATE_HOME XDG_RUNTIME_DIR
unset NIRI_SOCKET WAYLAND_DISPLAY ZELLIJ ZELLIJ_SESSION_NAME ZELLIJ_SOCKET_DIR ZELLIJ_CACHE_DIR SSH_AUTH_SOCK

packages=(
  ./internal/niri
  ./internal/niriipc
  ./internal/zellijlive
  ./internal/sourceinventory
  ./internal/sliceprotocol
  ./internal/sliceenv
  ./internal/sliceattach
  ./internal/slicetransport
  ./internal/slicerpc
  ./internal/slicelayout
  ./internal/slicecontroller
  ./internal/slicelaunch
  ./internal/hostleechsoak
  ./internal/mirror
  ./internal/resume
  ./internal/config
  ./cmd/redeem
)

export TERMINAL_REDEEMER_SOAK_ITERATIONS="${TERMINAL_REDEEMER_SOAK_ITERATIONS:-2000}"
export TERMINAL_REDEEMER_SOAK_SUMMARY="$matrix_home/soak-summary.json"
go test -count=1 "${packages[@]}"
python3 - "$TERMINAL_REDEEMER_SOAK_SUMMARY" <<'PY'
import json, pathlib, sys
value = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
assert value["schema_version"] == 1 and value["iterations"] >= 2000
assert value["secrets_included"] is False
assert all(v["observed"] <= v["limit"] for v in value["caps"].values())
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
assert required_caps <= set(value["caps"])
assert all(value["effects"].get(name) == 1 for name in ("host_session_create", "host_kitty_start", "host_placement", "host_source_commit", "routed_local_projection_launch"))
assert value["effects"].get("routed_transport_attempts") == 2
assert value["effects"].get("duplicate_active_projection", 0) == 0
assert value["resources"]["child_processes_remaining"] == 0
assert value["resources"]["prepared_namespaces_remaining"] == 0
assert value["resources"]["temporary_caches_remaining"] == 0
print(json.dumps({"soak":"passed","iterations":value["iterations"],"restarts":value["restarts"]}, separators=(",", ":")))
PY

bash scripts/tests/host-leech-layer-smoke.sh --require

if [[ "${RUN_HOST_LEECH_COVERAGE:-0}" == 1 ]]; then
  python3 scripts/tests/host-leech-coverage.py --output "$matrix_home/coverage.json"
  python3 - "$matrix_home/coverage.json" docs/testing/host-leech-coverage-baseline.json <<'PY'
import json, pathlib, sys
current_path, baseline_path = map(pathlib.Path, sys.argv[1:])
current_raw = current_path.read_bytes()
baseline_raw = baseline_path.read_bytes()
current = json.loads(current_raw)
baseline = json.loads(baseline_raw)
assert current["schema_version"] == 2
assert current["baseline_version"] == baseline["baseline_version"]
assert current == baseline, "coverage baseline drift requires regeneration, risk-family review, and a versioned baseline change"
assert current_raw == baseline_raw, "coverage baseline serialization drift"
print(json.dumps({"coverage":"exact","baseline_version":current["baseline_version"],"risk_families":len(current["packages"])}, separators=(",", ":")))
PY
fi

if [[ "${RUN_LOCKED_NIRI_CONTRACT_SPIKE:-0}" == 1 ]]; then
  : "${NIRI_BIN:?set by the locked flake check}"
  : "${PYTHON_BIN:?set by the locked flake check}"
  : "${EXPECTED_NIRI_VERSION:?set by the locked flake check}"
  env -u WAYLAND_DISPLAY -u WAYLAND_SOCKET -u NIRI_SOCKET -u SSH_AUTH_SOCK \
    bash scripts/spikes/niri-direct-ipc.sh --contract
fi

if [[ "${RUN_LOCKED_ZELLIJ_SPIKE:-0}" == 1 ]]; then
  : "${ZELLIJ_BIN:?set by the locked flake check}"
  : "${SCRIPT_BIN:?set by the locked flake check}"
  : "${TIMEOUT_BIN:?set by the locked flake check}"
  : "${EXPECTED_ZELLIJ_VERSION:?set by the locked flake check}"
  bash scripts/spikes/zellij-live-only-attachment.sh
fi
