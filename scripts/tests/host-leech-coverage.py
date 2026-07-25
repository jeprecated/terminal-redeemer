#!/usr/bin/env python3
"""Record deterministic host/leech risk-family coverage without a global gate."""
from __future__ import annotations

import argparse
import json
import os
import pathlib
import re
import subprocess
import tempfile

ROOT = pathlib.Path(__file__).resolve().parents[2]
RISK_FAMILIES = [
    ("protocol_acceptance", "./internal/sliceprotocol", ["Accept", "Decode", "RejectDuplicateKeys", "SemanticHash"]),
    ("inventory_authority", "./internal/sourceinventory", ["Build", "Snapshot", "Write"]),
    ("controller_lifecycle", "./internal/slicecontroller", ["ApplyEnvelope", "Tick", "Reconnect", "Compact"]),
    ("routed_host_journal", "./internal/slicerpc", ["Handle", "CreatePendingRouted", "Update", "resumeHostLaunch"]),
    ("routed_leech_intent", "./internal/slicelaunch", ["Route", "Reconnect", "Create", "Write"]),
    ("exact_attachment", "./internal/sliceattach", ["PreparePlannedExactSocket", "ValidatePreparedExactSocket", "Attach"]),
    ("transport_typing", "./internal/slicetransport", ["Call", "decodeResponse", "validateOutcome"]),
    ("spatial_policy", "./internal/slicelayout", ["Plan", "InitialLaunchOrder"]),
]
LINE = re.compile(r"^(?P<path>\S+):(?P<line>\d+):\s+(?P<function>\S+)\s+(?P<percent>[0-9.]+)%$")


def run(*argv: str, env: dict[str, str]) -> str:
    completed = subprocess.run(argv, cwd=ROOT, env=env, text=True, stdout=subprocess.PIPE,
                               stderr=subprocess.STDOUT, check=False)
    if completed.returncode:
        raise SystemExit(completed.stdout)
    return completed.stdout


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=pathlib.Path)
    parser.add_argument("--iterations", type=int, default=1)
    args = parser.parse_args()
    if not 1 <= args.iterations <= 10:
        raise SystemExit("coverage iterations must be within [1,10]")
    env = os.environ.copy()
    env.update({"GOTOOLCHAIN": "local", "GOPROXY": "off", "GOSUMDB": "off", "TMPDIR": "/tmp"})
    result: dict[str, object] = {
        "schema_version": 2,
        "baseline_version": "host-leech-v1-2026-07-26.2",
        "policy": "exact_reviewed_risk_family_and_named_function_baseline",
        "packages": [],
    }
    with tempfile.TemporaryDirectory(prefix="tr-coverage-", dir="/tmp") as tmp:
        for risk, package, required in RISK_FAMILIES:
            profile = pathlib.Path(tmp) / (risk + ".cover")
            run("go", "test", f"-count={args.iterations}", "-covermode=atomic", f"-coverprofile={profile}", package, env=env)
            report = run("go", "tool", "cover", f"-func={profile}", env=env)
            functions: dict[str, float] = {}
            total = None
            for raw in report.splitlines():
                if raw.startswith("total:"):
                    total = float(raw.rsplit(None, 1)[-1].rstrip("%"))
                    continue
                match = LINE.match(raw)
                if match:
                    functions[match.group("function")] = float(match.group("percent"))
            if total is None:
                raise SystemExit(f"missing total for {package}")
            named: dict[str, float] = {}
            for wanted in required:
                matches = [(name, value) for name, value in functions.items()
                           if name == wanted or name.endswith("." + wanted) or name.endswith(")." + wanted)]
                if not matches:
                    raise SystemExit(f"missing critical function {wanted} in {package}")
                named[wanted] = max(value for _, value in matches)
            result["packages"].append({
                "risk_family": risk,
                "package": package,
                "statement_percent": total,
                "critical_functions": named,
            })
    payload = json.dumps(result, sort_keys=True, separators=(",", ":")) + "\n"
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(payload, encoding="utf-8")
    else:
        print(payload, end="")


if __name__ == "__main__":
    main()
