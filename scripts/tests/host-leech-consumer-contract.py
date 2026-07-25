#!/usr/bin/env python3
"""Validate the host/leech contract and its schemas using only the Python stdlib."""

from __future__ import annotations

import copy
import json
import os
import pathlib
import re
import sys

ROOT = pathlib.Path(
    os.environ.get("TERMINAL_REDEEMER_SOURCE_ROOT", pathlib.Path(__file__).resolve().parents[2])
).resolve()
BASE = ROOT / "contracts" / "host-leech-slices" / "v1"


class SchemaError(ValueError):
    pass


def no_duplicates(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise ValueError(f"duplicate JSON key: {key}")
        value[key] = item
    return value


def load(name):
    path = BASE / name
    raw = path.read_bytes()
    if raw.decode("utf-8").encode("utf-8") != raw:
        raise ValueError(f"{name}: non-canonical UTF-8")
    return raw, json.loads(raw, object_pairs_hook=no_duplicates)


def require(value, expected, label):
    if value != expected:
        raise ValueError(f"{label}: got {value!r}, want {expected!r}")


def json_type_matches(value, expected):
    return {
        "object": isinstance(value, dict),
        "array": isinstance(value, list),
        "string": isinstance(value, str),
        "integer": isinstance(value, int) and not isinstance(value, bool),
        "number": isinstance(value, (int, float)) and not isinstance(value, bool),
        "boolean": isinstance(value, bool),
        "null": value is None,
    }.get(expected, False)


def resolve_pointer(root, pointer):
    if not pointer.startswith("#/"):
        raise SchemaError(f"unsupported non-local schema reference: {pointer}")
    value = root
    for segment in pointer[2:].split("/"):
        segment = segment.replace("~1", "/").replace("~0", "~")
        if not isinstance(value, dict) or segment not in value:
            raise SchemaError(f"unresolved schema reference: {pointer}")
        value = value[segment]
    return value


def validate_instance(instance, schema, root=None, path="$", depth=0):
    """Validate the strict Draft-2020-12 subset used by repository schemas."""
    if depth > 64:
        raise SchemaError(f"{path}: schema recursion limit")
    root = schema if root is None else root
    if not isinstance(schema, dict):
        raise SchemaError(f"{path}: schema must be an object")
    if "$ref" in schema:
        return validate_instance(instance, resolve_pointer(root, schema["$ref"]), root, path, depth + 1)
    if "const" in schema and instance != schema["const"]:
        raise SchemaError(f"{path}: value {instance!r} does not equal const {schema['const']!r}")
    if "enum" in schema and instance not in schema["enum"]:
        raise SchemaError(f"{path}: value is not in enum")
    if "type" in schema and not json_type_matches(instance, schema["type"]):
        raise SchemaError(f"{path}: expected {schema['type']}, got {type(instance).__name__}")

    if isinstance(instance, dict):
        required = schema.get("required", [])
        if not isinstance(required, list) or any(not isinstance(item, str) for item in required):
            raise SchemaError(f"{path}: malformed required declaration")
        missing = [item for item in required if item not in instance]
        if missing:
            raise SchemaError(f"{path}: missing required properties {missing}")
        properties = schema.get("properties", {})
        if not isinstance(properties, dict):
            raise SchemaError(f"{path}: malformed properties declaration")
        unknown = sorted(set(instance) - set(properties))
        if unknown and schema.get("additionalProperties") is False:
            raise SchemaError(f"{path}: additional properties {unknown}")
        for key, value in instance.items():
            if key in properties:
                validate_instance(value, properties[key], root, f"{path}.{key}", depth + 1)
            elif isinstance(schema.get("additionalProperties"), dict):
                validate_instance(value, schema["additionalProperties"], root, f"{path}.{key}", depth + 1)

    if isinstance(instance, list):
        if "minItems" in schema and len(instance) < schema["minItems"]:
            raise SchemaError(f"{path}: fewer than minItems")
        if "maxItems" in schema and len(instance) > schema["maxItems"]:
            raise SchemaError(f"{path}: more than maxItems")
        if schema.get("uniqueItems"):
            encoded = [json.dumps(item, sort_keys=True, separators=(",", ":")) for item in instance]
            if len(encoded) != len(set(encoded)):
                raise SchemaError(f"{path}: duplicate array item")
        if isinstance(schema.get("items"), dict):
            for index, value in enumerate(instance):
                validate_instance(value, schema["items"], root, f"{path}[{index}]", depth + 1)

    if isinstance(instance, str):
        if "minLength" in schema and len(instance) < schema["minLength"]:
            raise SchemaError(f"{path}: shorter than minLength")
        if "maxLength" in schema and len(instance) > schema["maxLength"]:
            raise SchemaError(f"{path}: longer than maxLength")
        if "pattern" in schema and re.search(schema["pattern"], instance) is None:
            raise SchemaError(f"{path}: does not match pattern")

    if isinstance(instance, (int, float)) and not isinstance(instance, bool):
        if "minimum" in schema and instance < schema["minimum"]:
            raise SchemaError(f"{path}: less than minimum")
        if "maximum" in schema and instance > schema["maximum"]:
            raise SchemaError(f"{path}: greater than maximum")


def wrong_value(value):
    if value is None:
        return "not-null"
    if isinstance(value, bool):
        return not value
    if isinstance(value, int):
        return value + 1
    if isinstance(value, str):
        return value + "-mutated"
    if isinstance(value, list):
        return [*value, "__mutated__"]
    if isinstance(value, dict):
        return {**value, "__unexpected__": True}
    raise TypeError(f"no mutation for {type(value)}")


def expect_invalid(instance, schema, label):
    try:
        validate_instance(instance, schema)
    except SchemaError:
        return
    raise ValueError(f"negative schema mutation was accepted: {label}")


def validate_negative_mutations(instance, schema, object_paths, label):
    """Prove required/const/additionalProperties gates for every critical object."""
    for path in object_paths:
        target = instance
        target_schema = schema
        path_label = label
        for segment in path:
            target = target[segment]
            target_schema = target_schema["properties"][segment]
            path_label += f".{segment}"
        if not isinstance(target, dict):
            raise ValueError(f"negative test target is not an object: {path_label}")
        for key in target_schema.get("required", []):
            mutated = copy.deepcopy(instance)
            cursor = mutated
            for segment in path:
                cursor = cursor[segment]
            cursor.pop(key)
            expect_invalid(mutated, schema, f"{path_label} missing {key}")
        for key, value in target.items():
            mutated = copy.deepcopy(instance)
            cursor = mutated
            for segment in path:
                cursor = cursor[segment]
            cursor[key] = wrong_value(value)
            expect_invalid(mutated, schema, f"{path_label} changed {key}")
        mutated = copy.deepcopy(instance)
        cursor = mutated
        for segment in path:
            cursor = cursor[segment]
        cursor["__unexpected__"] = True
        expect_invalid(mutated, schema, f"{path_label} additional property")


def validate_packaged_artifacts():
    package_root = os.environ.get("TERMINAL_REDEEMER_CONTRACT_PACKAGE_ROOT")
    if not package_root:
        return
    base = pathlib.Path(package_root).resolve()
    if not base.is_dir():
        raise ValueError(f"contract package root is not a directory: {base}")
    for member in (
        "consumer-contract.json",
        "consumer-contract.schema.json",
        "niri-bindings.kdl.in",
    ):
        candidate = (base / member).resolve()
        if candidate.parent != base or not candidate.is_file():
            raise ValueError(f"packaged contract member is missing: {member}")
        require(candidate.read_bytes(), (BASE / member).read_bytes(), f"packaged {member} bytes")
    generated = base / "niri-bindings.kdl"
    if not generated.is_file():
        raise ValueError("packaged generated Niri bindings are missing")


def validate_relative_markdown_links():
    markdown_files = [ROOT / "README.md", *sorted((ROOT / "docs").rglob("*.md"))]
    pattern = re.compile(r"(?<!!)\[[^\]]*\]\(([^)]+)\)")
    for document in markdown_files:
        text = document.read_text(encoding="utf-8")
        for match in pattern.finditer(text):
            target = match.group(1).strip()
            if target.startswith("<") and target.endswith(">"):
                target = target[1:-1]
            target = target.split(" ", 1)[0].split("#", 1)[0]
            if not target or target.startswith(("http://", "https://", "mailto:", "/")):
                continue
            resolved = (document.parent / target).resolve()
            if not resolved.exists():
                raise ValueError(f"{document.relative_to(ROOT)}: missing relative link {target}")


def main():
    _, contract = load("consumer-contract.json")
    _, contract_schema = load("consumer-contract.schema.json")

    require(contract_schema["$schema"], "https://json-schema.org/draft/2020-12/schema", "contract schema dialect")
    require(contract_schema["type"], "object", "contract schema root")
    require(contract_schema["additionalProperties"], False, "contract schema unknown fields")
    if not contract_schema.get("required"):
        raise ValueError("contract schema: no required fields")

    # Actual offline instance validation, followed by adversarial mutation gates.
    validate_instance(contract, contract_schema)
    validate_negative_mutations(
        contract,
        contract_schema,
        [
            (),
            ("protocol",),
            ("compatibility",),
            ("defaults",),
            ("authority",),
            ("drops",),
            ("revisions",),
            ("commands",),
            ("module",),
            ("configuration",),
            ("integration",),
            ("limitations",),
            ("rollout",),
        ],
        "contract",
    )
    future_mutation = copy.deepcopy(contract)
    future_mutation["future_work"] = list(reversed(future_mutation["future_work"]))
    expect_invalid(future_mutation, contract_schema, "contract.future_work changed")
    expected_protocol = {
        "inventory_schema_versions": [1],
        "rpc_schema_versions": [1],
        "controller_schema_versions": [2],
        "workspace_normalization": "unicode-nfkc-fold-v1",
    }
    expected_compatibility = {
        "niri_version": "25.11",
        "zellij_version": "0.43.1",
        "topology": "one_active_output_per_machine",
    }
    expected_defaults = {
        "leech_mode_enabled": False,
        "controller_enabled": False,
        "slice_clipboard_enabled": False,
        "authority_mode": "host_location",
        "leech_write_authorized": False,
        "poll_interval": "2s",
        "control_timeout": "5s",
        "retry_window": "30s",
        "source_gone_grace": "5s",
        "source_gone_confirmations": 2,
    }
    expected_authority = {
        "supported_modes": ["host_location"],
        "converged_properties": ["workspace", "floating_or_tiled", "proportional_width", "proportional_height"],
        "local_supported_drift_policy": "revert_to_host",
        "order_policy": "initial_best_effort_then_observation_only",
        "leech_location_configurable": False,
        "host_spatial_writeback_available": False,
    }
    expected_drops = {
        "key": "exact_verified_zellij_session_id",
        "survives_source_replacement": True,
        "survives_source_epoch_replacement": True,
        "survives_headless_while_session_live": True,
        "early_clear_operations": ["reopen", "undo"],
        "automatic_expiry_requires": ["accepted_higher_complete_revisions", "consecutive_confirmed_session_absence", "committed_grace_elapsed"],
        "non_authoritative_observations_do_not_advance": True,
    }
    expected_revisions = {
        "complete_poll_advances_revision": True,
        "unchanged_semantics_still_advances_revision": True,
        "same_revision_same_semantics": "idempotent_replay",
        "same_revision_different_semantics": "conflict",
        "non_authoritative_observations": ["degraded", "duplicate", "stale", "conflicting", "retired_epoch", "transport_disconnected"],
    }
    expected_commands = {
        "inventory_init": ["$REDEEM", "slice", "inventory", "init"],
        "inventory_snapshot": ["$REDEEM", "slice", "inventory", "snapshot", "--accept-schema-version", "1"],
        "rpc": ["$REDEEM", "slice", "rpc"],
        "launch": ["$REDEEM", "slice", "launch"],
        "launch_reconnect": ["$REDEEM", "slice", "launch", "--reconnect-token", "$TOKEN"],
        "close_focused": ["$REDEEM", "slice", "close-focused"],
        "controller_init": ["$REDEEM", "slice", "controller", "init"],
        "controller_run": ["$REDEEM", "slice", "controller", "run"],
        "controller_status": ["$REDEEM", "slice", "controller", "status"],
        "controller_operations": ["workspace-add", "workspace-remove", "pickup", "drop", "close", "reopen", "undo", "reconnect", "launch-handoff"],
        "mode_enable": ["$REDEEM", "slice", "mode", "enable"],
        "mode_disable": ["$REDEEM", "slice", "mode", "disable"],
        "mode_status": ["$REDEEM", "slice", "mode", "status"],
        "legacy_attach": ["$REDEEM", "mirror", "open", "--mode", "attach"],
    }
    expected_module = {
        "home_manager": "homeManagerModules.terminal-redeemer",
        "nixos": "nixosModules.terminal-redeemer",
        "package": "packages.x86_64-linux.terminal-redeemer",
        "app": "apps.x86_64-linux.redeem",
        "contract_package": "packages.x86_64-linux.host-leech-consumer-contract",
        "contract_library": "lib.sliceConsumerContract",
    }
    expected_rollout = {
        "installs_niri_bindings": False,
        "legacy_attach_retained": True,
        "watch_supported": False,
        "automatic_local_fallback_after_remote_intent": False,
    }
    expected_configuration = {
        "namespace": "programs.terminal-redeemer.slice",
        "typed_options": [
            "leechMode.enable", "sourceHost", "selfCommand", "kittyCommand", "transportCommand",
            "transportOptions", "rpcCommand", "zellijCommand", "niriCommand", "systemctlCommand",
            "requestTimeout", "keepaliveInterval", "keepaliveCount", "retryMaxAttempts",
            "retryInitialBackoff", "retryMaxBackoff", "attachPrivateRoot", "attachShimCache",
            "controller.enable", "controller.hostID", "controller.leechID", "controller.pollInterval",
            "controller.controlTimeout", "controller.retryWindow", "controller.sourceGoneGrace",
            "controller.sourceGoneConfirmations",
        ],
        "read_only_options": ["launchCommand", "closeFocusedCommand", "niriIntegrationFragment", "clipboard.enabled"],
        "unsupported_options": ["controller.authorityMode", "controller.leechWriteAuthorized"],
    }
    expected_integration = {
        "launch_helper_option": "programs.terminal-redeemer.slice.launchCommand",
        "close_helper_option": "programs.terminal-redeemer.slice.closeFocusedCommand",
        "generated_niri_option": "programs.terminal-redeemer.slice.niriIntegrationFragment",
        "packaged_niri_path": "share/terminal-redeemer/host-leech-slices/v1/niri-bindings.kdl",
        "launch_binding": "Mod+Return",
        "close_binding": "Mod+W",
        "bindings_installed_automatically": False,
    }
    expected_limitations = {
        "zellij_shared_minimum_client_grid_reflow": True,
        "spatial_placement": "approximate_proportional",
        "pinned_version_coupling": True,
        "ambiguous_transport_may_have_created_host_work": True,
        "routed_launch_process_correlation_can_remain_pending": True,
    }
    expected_future = [
        "exact_live_column_order",
        "multi_monitor_topology",
        "slice_clipboard_sync",
        "named_slices",
        "read_only_watch_projection",
    ]
    require(contract["protocol"], expected_protocol, "protocol surface")
    require(contract["compatibility"], expected_compatibility, "compatibility surface")
    require(contract["defaults"], expected_defaults, "defaults surface")
    require(contract["authority"], expected_authority, "authority surface")
    require(contract["drops"], expected_drops, "drop surface")
    require(contract["revisions"], expected_revisions, "revision surface")
    require(contract["commands"], expected_commands, "command surface")
    require(contract["module"], expected_module, "module surface")
    require(contract["configuration"], expected_configuration, "configuration surface")
    require(contract["integration"], expected_integration, "integration surface")
    require(contract["limitations"], expected_limitations, "limitation surface")
    require(contract["rollout"], expected_rollout, "rollout surface")
    require(contract["future_work"], expected_future, "future-work surface")
    validate_packaged_artifacts()

    template = (BASE / "niri-bindings.kdl.in").read_text(encoding="utf-8")
    required_lines = [
        'Mod+Return { spawn "@REDEEM@" "slice" "launch"; }',
        'Mod+W { spawn "@REDEEM@" "slice" "close-focused"; }',
    ]
    for line in required_lines:
        if line not in template:
            raise ValueError(f"Niri template missing exact argv: {line}")
    if re.search(r"\b(sh|bash)\b|-c\b", template):
        raise ValueError("Niri template must not introduce a shell")

    readme = (ROOT / "README.md").read_text(encoding="utf-8")
    if "docs/HOST_LEECH_READINESS.md" not in readme:
        raise ValueError("README does not link readiness contract")
    validate_relative_markdown_links()

    print("PASS: the consumer contract and negative mutations satisfy the strict offline schema gate")
    if os.environ.get("TERMINAL_REDEEMER_CONTRACT_PACKAGE_ROOT"):
        print("PASS: packaged technical contract members match source bytes")
    print("PASS: host/leech technical defaults, compatibility, commands, and templates are validated")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (KeyError, OSError, UnicodeError, ValueError, json.JSONDecodeError) as exc:
        print(f"consumer contract validation failed: {exc}", file=sys.stderr)
        raise SystemExit(1)
