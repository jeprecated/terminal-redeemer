#!/usr/bin/env bash
set -euo pipefail

niri_bin=${NIRI_BIN:-niri}
kitty_bin=${KITTY_BIN:-kitty}
python_bin=${PYTHON_BIN:-python3}
expected_version=${EXPECTED_NIRI_VERSION:-25.11}
script_dir=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
probe=${NIRI_PROBE:-$script_dir/niri-direct-ipc-probe.py}
fixture_dir=${NIRI_FIXTURE_DIR:-$script_dir/../../internal/niri/testdata}
mode=live
case ${1:-} in
  "") ;;
  --contract) mode=contract ;;
  *) printf 'usage: %s [--contract]\n' "$0" >&2; exit 2 ;;
esac

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

version_output=$($niri_bin --version)
[[ $version_output == niri\ "$expected_version"* ]] || fail "expected Niri $expected_version, got: $version_output"

if [[ $mode == contract ]]; then
  "$python_bin" - "$probe" "$fixture_dir" "$expected_version" <<'PY'
import ast,copy,importlib.util,io,json,math,pathlib,sys
probe=pathlib.Path(sys.argv[1])
fixtures=pathlib.Path(sys.argv[2])
expected_version=sys.argv[3]
ast.parse(probe.read_text(), filename=str(probe))
spec=importlib.util.spec_from_file_location('niri_direct_ipc_probe',probe)
probe_module=importlib.util.module_from_spec(spec); spec.loader.exec_module(probe_module)

def require(condition, message):
    if not condition:
        raise AssertionError(message)

def positive_id(value, label):
    require(type(value) is int and 0 < value <= 2**64-1, f'{label} must be a positive uint64')

def validate_events(lines):
    require(lines[0] == {'Ok':'Handled'}, 'initial event-stream reply')
    require(list(lines[1]) == ['WorkspacesChanged'], 'workspace replay event')
    require(list(lines[2]) == ['WindowsChanged'], 'window replay event')
    require(lines[-1] == {'ConfigLoaded':{'failed':False}}, 'successful ConfigLoaded sentinel')

def validate_actions(actions):
    variants=['SetWorkspaceName','SetWorkspaceName','MoveWindowToWorkspace','MoveWindowToTiling','MoveWindowToFloating','SetWindowWidth','SetWindowHeight']
    require(len(actions) == len(variants), 'complete action matrix')
    bodies=[]
    for item,variant in zip(actions,variants):
        require(set(item) == {'request','reply'} and item['reply'] == {'Ok':'Handled'}, f'{variant} envelope/reply')
        require(set(item['request']) == {'Action'} and list(item['request']['Action']) == [variant], f'{variant} action variant')
        bodies.append(item['request']['Action'][variant])
    for index,(name,wid) in enumerate((('tr-spike',2),('TR-SPIKE',3))):
        body=bodies[index]
        require(set(body) == {'name','workspace'} and body['name'] == name, f'workspace-name action {index+1}')
        require(set(body['workspace']) == {'Id'} and body['workspace']['Id'] == wid, f'workspace reference {index+1}')
        positive_id(body['workspace']['Id'], f'workspace reference {index+1}')
    move=bodies[2]
    require(set(move) == {'window_id','reference','focus'} and move['window_id'] == 42, 'move window shape/id')
    require(set(move['reference']) == {'Id'} and move['reference']['Id'] == 2, 'move workspace reference')
    positive_id(move['window_id'], 'move window id'); positive_id(move['reference']['Id'], 'move workspace id')
    require(move['focus'] is False, 'move must preserve focus')
    for index,label in ((3,'tiled'),(4,'floating')):
        body=bodies[index]
        require(set(body) == {'id'} and body['id'] == 42, f'{label} exact window id')
        positive_id(body['id'], f'{label} window id')
    for index,(label,expected) in enumerate((('width',45.0),('height',40.0)), start=5):
        body=bodies[index]
        require(set(body) == {'id','change'} and body['id'] == 42, f'{label} exact window id')
        positive_id(body['id'], f'{label} window id')
        require(set(body['change']) == {'SetProportion'}, f'{label} change variant')
        value=body['change']['SetProportion']
        require(type(value) in (int,float) and math.isfinite(value) and 1.0 <= value <= 100.0 and value == expected, f'{label} bounded proportion')

lines=[json.loads(line) for line in (fixtures/'ipc-event-stream.jsonl').read_text().splitlines() if line]
validate_events(lines)
complete=json.loads((fixtures/'ipc-complete-state.json').read_text())
outputs=set(complete['outputs'])
workspaces={item['id'] for item in complete['workspaces']}
require(all(item.get('output') is None or item['output'] in outputs for item in complete['workspaces']), 'workspace output joins')
require(all(item.get('workspace_id') is None or item['workspace_id'] in workspaces for item in complete['windows']), 'window workspace joins')
actions=[json.loads(line) for line in (fixtures/'ipc-actions.jsonl').read_text().splitlines() if line]
validate_actions(actions)

# Adversarial assertions prove each security-relevant check rejects malformed
# fixture data, especially the formerly accepted failed ConfigLoaded sentinel.
def rejects(fn, value):
    try: fn(value)
    except (AssertionError,KeyError,RuntimeError,TypeError): return True
    return False
bad_events=[json.loads(line) for line in (fixtures/'ipc-event-stream-failed-config.jsonl').read_text().splitlines() if line]
require(rejects(validate_events,bad_events), 'failed ConfigLoaded adversarial fixture was accepted')

# Exercise the live probe implementation itself, not only the fixture contract.
class FakeConnection:
    def __init__(self, events):
        self.payload=('\n'.join(json.dumps(item,separators=(',',':')) for item in events)+'\n').encode()
    def sendall(self, _payload): pass
    def makefile(self, _mode): return io.BytesIO(self.payload)
    def close(self): pass

def probe_events(events):
    client=probe_module.NiriClient('/unused',1)
    client.connect=lambda: FakeConnection(events)
    return client.initial_events()

successful=[{'Ok':'Handled'},{'WorkspacesChanged':{'workspaces':[]}},{'WindowsChanged':{'windows':[]}},{'ConfigLoaded':{'failed':False}}]
probe_events(successful)
failed=copy.deepcopy(successful); failed[-1]['ConfigLoaded']['failed']=True
require(rejects(probe_events,failed), 'live probe accepted ConfigLoaded.failed:true')
for label,mutate in (
    ('workspace id',lambda a: a[0]['request']['Action']['SetWorkspaceName']['workspace'].__setitem__('Id',0)),
    ('focus',lambda a: a[2]['request']['Action']['MoveWindowToWorkspace'].__setitem__('focus',True)),
    ('tiled id',lambda a: a[3]['request']['Action']['MoveWindowToTiling'].__setitem__('id',-1)),
    ('floating id',lambda a: a[4]['request']['Action']['MoveWindowToFloating'].__setitem__('id',0)),
    ('width bound',lambda a: a[5]['request']['Action']['SetWindowWidth']['change'].__setitem__('SetProportion',101.0)),
    ('height reference',lambda a: a[6]['request']['Action']['SetWindowHeight'].__setitem__('id',43)),
):
    bad=copy.deepcopy(actions); mutate(bad)
    require(rejects(validate_actions,bad), f'{label} adversary was accepted')
print(f'PASS: hermetic Niri {expected_version} IPC fixture, successful sentinel, exact/bounded action contract, and adversarial rejections verified')
PY
  exit 0
fi

[[ -n ${WAYLAND_DISPLAY:-} || -n ${WAYLAND_SOCKET:-} ]] || fail "a parent Wayland session is required for the nested Niri spike"
root=$(mktemp -d "${TMPDIR:-/tmp}/terminal-redeemer-niri-spike.XXXXXX")
results=${SPIKE_OUTPUT_DIR:-$root/results}
mkdir -p "$results"
pids=()

cleanup() {
  for pid in "${pids[@]:-}"; do
    kill "$pid" >/dev/null 2>&1 || true
    wait "$pid" >/dev/null 2>&1 || true
  done
  if [[ ${KEEP_SPIKE_OUTPUT:-0} != 1 ]]; then
    rm -rf "$root"
  else
    printf 'kept nested Niri spike workspace: %s\n' "$root"
  fi
}
trap cleanup EXIT

start_nested() {
  local label=$1 mutate=$2
  local run_dir=$root/$label
  local output_dir=$results/$label
  mkdir -p "$run_dir" "$output_dir"
  : >"$run_dir/config.kdl"
  cat >"$run_dir/child.sh" <<EOF
#!/bin/sh
printf 'NIRI_SOCKET=%s\nWAYLAND_DISPLAY=%s\n' "\$NIRI_SOCKET" "\$WAYLAND_DISPLAY" > '$run_dir/endpoint.env'
exec '$kitty_bin' --config NONE --class tr-niri-spike-$label --title tr-niri-spike-$label sh -c 'sleep 120'
EOF
  chmod +x "$run_dir/child.sh"

  "$niri_bin" -c "$run_dir/config.kdl" -- "$run_dir/child.sh" >"$run_dir/niri.log" 2>&1 &
  local pid=$!
  pids+=("$pid")
  for _ in $(seq 1 150); do
    [[ -s $run_dir/endpoint.env ]] && break
    kill -0 "$pid" >/dev/null 2>&1 || {
      tail -100 "$run_dir/niri.log" >&2
      fail "nested Niri $label exited before publishing its endpoint"
    }
    sleep 0.1
  done
  [[ -s $run_dir/endpoint.env ]] || fail "nested Niri $label did not publish its endpoint"

  local inner_socket
  inner_socket=$(while IFS='=' read -r key value; do
    if [[ $key == NIRI_SOCKET ]]; then
      printf '%s' "$value"
      break
    fi
  done <"$run_dir/endpoint.env")
  for _ in $(seq 1 100); do
    [[ -S $inner_socket ]] && break
    kill -0 "$pid" >/dev/null 2>&1 || break
    sleep 0.05
  done
  [[ -S $inner_socket ]] || fail "nested Niri $label socket is unavailable"

  local args=("$python_bin" "$probe" --socket "$inner_socket" --output-dir "$output_dir" --timeout 8)
  [[ $mutate == yes ]] && args+=(--mutate)
  "${args[@]}"

  kill "$pid" >/dev/null 2>&1 || true
  wait "$pid" >/dev/null 2>&1 || true
  pids=("${pids[@]:0:${#pids[@]}-1}")
}

start_nested first yes
start_nested second no

"$python_bin" - "$results/first/result.json" "$results/second/result.json" <<'PY'
import json,sys
first=json.load(open(sys.argv[1]))
second=json.load(open(sys.argv[2]))
assert first["source_identity"]["boot_digest"] == second["source_identity"]["boot_digest"], "boot identity changed during spike"
assert first["source_identity"]["instance_digest"] != second["source_identity"]["instance_digest"], "Niri restart did not rotate source-instance identity"
assert first["mutations_verified"] is True
assert second["mutations_verified"] is False
print("PASS: Niri direct-IPC inventory, source-instance rotation, and safe MVP mutations verified")
PY
