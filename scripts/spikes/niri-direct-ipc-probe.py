#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
import socket
import stat
import time
from pathlib import Path


def encode(value):
    return json.dumps(value, separators=(",", ":"))


class NiriClient:
    def __init__(self, socket_path, timeout):
        self.socket_path = socket_path
        self.timeout = timeout

    def connect(self):
        conn = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        conn.settimeout(self.timeout)
        conn.connect(self.socket_path)
        return conn

    def request(self, value):
        conn = self.connect()
        try:
            conn.sendall(encode(value).encode() + b"\n")
            line = conn.makefile("rb").readline()
            if not line:
                raise RuntimeError("Niri closed the socket before replying")
            reply = json.loads(line)
            if "Err" in reply:
                raise RuntimeError(f"Niri request failed: {reply['Err']}")
            return reply
        finally:
            conn.close()

    def initial_events(self):
        conn = self.connect()
        lines = []
        try:
            conn.sendall(b'"EventStream"\n')
            stream = conn.makefile("rb")
            reply = stream.readline()
            if not reply:
                raise RuntimeError("Niri closed the event stream before replying")
            lines.append(reply.decode().rstrip("\n"))
            decoded_reply = json.loads(reply)
            if decoded_reply != {"Ok": "Handled"}:
                raise RuntimeError(f"unexpected event-stream reply: {decoded_reply}")

            saw_workspaces = False
            saw_windows = False
            events = []
            while True:
                line = stream.readline()
                if not line:
                    raise RuntimeError("Niri closed the initial replay before ConfigLoaded")
                lines.append(line.decode().rstrip("\n"))
                event = json.loads(line)
                events.append(event)
                saw_workspaces = saw_workspaces or "WorkspacesChanged" in event
                saw_windows = saw_windows or "WindowsChanged" in event
                if "ConfigLoaded" in event:
                    config_loaded = event["ConfigLoaded"]
                    if (
                        not isinstance(config_loaded, dict)
                        or type(config_loaded.get("failed")) is not bool
                        or config_loaded["failed"]
                    ):
                        raise RuntimeError(
                            f"initial replay ended with failed or malformed ConfigLoaded: {event}"
                        )
                    break
            if not saw_workspaces or not saw_windows:
                raise RuntimeError("initial replay did not contain workspace and window snapshots")
            return lines, events
        finally:
            conn.close()


def ok_value(reply, variant):
    ok = reply.get("Ok")
    if not isinstance(ok, dict) or variant not in ok:
        raise RuntimeError(f"expected Ok.{variant}, got {reply}")
    return ok[variant]


def query_state(client):
    return {
        "outputs": ok_value(client.request("Outputs"), "Outputs"),
        "workspaces": ok_value(client.request("Workspaces"), "Workspaces"),
        "windows": ok_value(client.request("Windows"), "Windows"),
    }


def validate_state(state):
    reasons = []
    output_names = set(state["outputs"])
    workspace_ids = {workspace["id"] for workspace in state["workspaces"]}
    for workspace in state["workspaces"]:
        output = workspace.get("output")
        if output is not None and output not in output_names:
            reasons.append(f"workspace {workspace['id']} references missing output {output}")
    for window in state["windows"]:
        workspace_id = window.get("workspace_id")
        if workspace_id is not None and workspace_id not in workspace_ids:
            reasons.append(f"window {window['id']} references missing workspace {workspace_id}")
    return reasons


def wait_state(client, predicate, timeout, description):
    deadline = time.monotonic() + timeout
    last = None
    while time.monotonic() < deadline:
        last = query_state(client)
        if predicate(last):
            return last
        time.sleep(0.05)
    raise RuntimeError(f"timed out waiting for {description}; last state: {last}")


def source_identity(socket_path):
    info = os.lstat(socket_path)
    if not stat.S_ISSOCK(info.st_mode):
        raise RuntimeError("NIRI_SOCKET is not a Unix socket")
    boot_id = Path("/proc/sys/kernel/random/boot_id").read_text().strip()
    boot_digest = hashlib.sha256(boot_id.encode()).hexdigest()
    instance_digest = hashlib.sha256(
        f"{boot_id}:{info.st_dev}:{info.st_ino}".encode()
    ).hexdigest()
    return {
        "boot_digest": boot_digest,
        "instance_digest": instance_digest,
    }


def record_action(client, actions, request):
    reply = client.request(request)
    actions.append({"request": request, "reply": reply})
    return reply


def run_mutations(client, initial, actions, timeout):
    state = wait_state(
        client,
        lambda value: len(value["windows"]) == 1 and len(value["outputs"]) == 1,
        timeout,
        "one nested test window and one output",
    )
    if validate_state(state):
        raise RuntimeError(f"initial state is inconsistent: {validate_state(state)}")

    output_name = next(iter(state["outputs"]))
    window = state["windows"][0]
    window_id = window["id"]
    focused_workspace = next(workspace["id"] for workspace in state["workspaces"] if workspace["is_focused"])
    occupied = {item.get("workspace_id") for item in state["windows"]}
    trailing = max(
        (
            workspace
            for workspace in state["workspaces"]
            if workspace.get("output") == output_name
            and workspace.get("name") is None
            and workspace["id"] not in occupied
        ),
        key=lambda workspace: workspace["idx"],
    )
    workspace_id = trailing["id"]
    before_count = len(state["workspaces"])

    record_action(
        client,
        actions,
        {"Action": {"SetWorkspaceName": {"name": "tr-spike", "workspace": {"Id": workspace_id}}}},
    )
    named = wait_state(
        client,
        lambda value: any(
            item["id"] == workspace_id and item.get("name") == "tr-spike"
            for item in value["workspaces"]
        )
        and len(value["workspaces"]) == before_count + 1,
        timeout,
        "named workspace and replacement trailing workspace",
    )

    occupied = {item.get("workspace_id") for item in named["windows"]}
    replacement = max(
        (
            workspace
            for workspace in named["workspaces"]
            if workspace.get("output") == output_name
            and workspace.get("name") is None
            and workspace["id"] not in occupied
        ),
        key=lambda workspace: workspace["idx"],
    )
    replacement_id = replacement["id"]
    record_action(
        client,
        actions,
        {"Action": {"SetWorkspaceName": {"name": "TR-SPIKE", "workspace": {"Id": replacement_id}}}},
    )
    duplicate = None
    for _ in range(10):
        duplicate = query_state(client)
        duplicate_target = next(item for item in duplicate["workspaces"] if item["id"] == replacement_id)
        if duplicate_target.get("name") is not None:
            raise RuntimeError("case-colliding workspace name was not silently rejected")
        time.sleep(0.05)

    record_action(
        client,
        actions,
        {
            "Action": {
                "MoveWindowToWorkspace": {
                    "window_id": window_id,
                    "reference": {"Id": workspace_id},
                    "focus": False,
                }
            }
        },
    )
    moved = wait_state(
        client,
        lambda value: value["windows"][0].get("workspace_id") == workspace_id
        and next(item["id"] for item in value["workspaces"] if item["is_focused"]) == focused_workspace,
        timeout,
        "targeted workspace move without focus following",
    )

    record_action(client, actions, {"Action": {"MoveWindowToTiling": {"id": window_id}}})
    record_action(client, actions, {"Action": {"MoveWindowToFloating": {"id": window_id}}})
    record_action(
        client,
        actions,
        {"Action": {"SetWindowWidth": {"id": window_id, "change": {"SetProportion": 45.0}}}},
    )
    record_action(
        client,
        actions,
        {"Action": {"SetWindowHeight": {"id": window_id, "change": {"SetProportion": 40.0}}}},
    )
    final = wait_state(
        client,
        lambda value: value["windows"][0].get("is_floating") is True
        and value["windows"][0]["layout"]["window_size"][0]
        < value["outputs"][output_name]["logical"]["width"]
        and value["windows"][0]["layout"]["window_size"][1]
        < value["outputs"][output_name]["logical"]["height"],
        timeout,
        "floating and proportional size mutation",
    )

    return {
        "initial": state,
        "after_workspace_create": named,
        "after_move": moved,
        "final": final,
        "workspace_id": workspace_id,
        "window_id": window_id,
        "focused_workspace_id": focused_workspace,
    }


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--socket", required=True)
    parser.add_argument("--output-dir", required=True)
    parser.add_argument("--timeout", type=float, default=5.0)
    parser.add_argument("--mutate", action="store_true")
    args = parser.parse_args()

    output_dir = Path(args.output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)
    client = NiriClient(args.socket, args.timeout)
    identity = source_identity(args.socket)
    lines, events = client.initial_events()
    (output_dir / "event-stream.jsonl").write_text("\n".join(lines) + "\n")
    initial = query_state(client)
    reasons = validate_state(initial)
    write_json(output_dir / "initial-state.json", initial)

    actions = []
    mutation_result = None
    if args.mutate:
        mutation_result = run_mutations(client, initial, actions, args.timeout)
        write_json(output_dir / "mutation-state.json", mutation_result)
        (output_dir / "actions.jsonl").write_text(
            "\n".join(encode(action) for action in actions) + "\n"
        )

    result = {
        "source_identity": identity,
        "event_count_through_config_loaded": len(events),
        "initial_validation_reasons": reasons,
        "mutations_verified": mutation_result is not None,
    }
    write_json(output_dir / "result.json", result)
    if reasons:
        raise RuntimeError(f"initial snapshot is degraded: {reasons}")
    print(json.dumps(result, sort_keys=True))


if __name__ == "__main__":
    main()
