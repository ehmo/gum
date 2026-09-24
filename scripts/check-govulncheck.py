#!/usr/bin/env python3
"""Fail on any govulncheck finding gum's build can reach, and on any
module-only finding that scripts/govulncheck-allowlist.json does not record.

govulncheck reports a finding at the most precise level it can prove. A symbol
or package level finding means the vulnerable code is in gum's build, and no
allowlist entry can excuse it. A module level finding means the advisory covers
import paths gum never builds, so the vulnerability cannot execute; those are
recorded one by one with the reason the requirement stays.
"""
from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parent.parent
MODULE_DIR = ROOT / "apps" / "gum"
ALLOWLIST = ROOT / "scripts" / "govulncheck-allowlist.json"
ALLOWLIST_REL = "scripts/govulncheck-allowlist.json"

# govulncheck exits 3 when it reports findings. JSON output exits 0 either way.
FINDINGS_EXIT = 3

REQUIRED_ENTRY_FIELDS = ("id", "module", "reason", "recorded")


def fail(message: str) -> None:
    print(f"govulncheck gate: {message}", file=sys.stderr)
    raise SystemExit(1)


def run_govulncheck(binary: str) -> str:
    command = [binary, "-format", "json", "-scan", "symbol", "./..."]
    try:
        proc = subprocess.run(
            command,
            cwd=str(MODULE_DIR),
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
    except FileNotFoundError:
        fail(f"cannot run {binary}; install golang.org/x/vuln/cmd/govulncheck")
    if proc.returncode not in (0, FINDINGS_EXIT):
        sys.stderr.write(proc.stderr)
        fail(f"{binary} exited {proc.returncode}")
    return proc.stdout


def decode_stream(raw: str) -> list[dict]:
    """govulncheck writes concatenated JSON objects, not a JSON array."""
    decoder = json.JSONDecoder()
    objects: list[dict] = []
    index = 0
    end = len(raw)
    while index < end:
        while index < end and raw[index].isspace():
            index += 1
        if index >= end:
            break
        try:
            obj, index = decoder.raw_decode(raw, index)
        except json.JSONDecodeError as err:
            fail(f"cannot parse govulncheck output at offset {index}: {err}")
        objects.append(obj)
    return objects


def finding_level(finding: dict) -> str:
    trace = finding.get("trace") or []
    if not trace:
        return "module"
    frame = trace[0]
    if frame.get("function"):
        return "symbol"
    if frame.get("package"):
        return "package"
    return "module"


def read_allowlist() -> dict[str, dict]:
    if not ALLOWLIST.exists():
        fail(f"{ALLOWLIST_REL} is missing")
    data = json.loads(ALLOWLIST.read_text(encoding="utf-8"))
    entries = data.get("module_only")
    if not isinstance(entries, list):
        fail(f"{ALLOWLIST_REL} needs a module_only list")
    allowed: dict[str, dict] = {}
    for entry in entries:
        if not isinstance(entry, dict):
            fail(f"{ALLOWLIST_REL} entries must be objects")
        for field in REQUIRED_ENTRY_FIELDS:
            if not entry.get(field):
                fail(f"{ALLOWLIST_REL} entry {entry.get('id', '<no id>')} is missing {field}")
        if entry["id"] in allowed:
            fail(f"{ALLOWLIST_REL} lists {entry['id']} twice")
        allowed[entry["id"]] = entry
    return allowed


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--govulncheck",
        default=os.environ.get("GOVULNCHECK", "govulncheck"),
        help="govulncheck binary; defaults to $GOVULNCHECK, else the one on PATH",
    )
    args = parser.parse_args()

    binary = shutil.which(args.govulncheck) or args.govulncheck
    objects = decode_stream(run_govulncheck(binary))
    findings = [obj["finding"] for obj in objects if "finding" in obj]

    reachable: dict[str, str] = {}
    module_only: dict[str, str] = {}
    for finding in findings:
        osv = finding.get("osv") or "<unknown advisory>"
        level = finding_level(finding)
        if level == "module":
            trace = finding.get("trace") or [{}]
            module_only.setdefault(osv, trace[0].get("module") or "<unknown module>")
            continue
        reachable[osv] = level

    # A precise finding outranks a module level one for the same advisory.
    for osv in reachable:
        module_only.pop(osv, None)

    if reachable:
        for osv, level in sorted(reachable.items()):
            print(f"govulncheck gate: {osv} is reachable at {level} level", file=sys.stderr)
        noun = "vulnerability" if len(reachable) == 1 else "vulnerabilities"
        fail(f"{len(reachable)} reachable {noun}; no allowlist entry covers these")

    allowed = read_allowlist()

    unrecorded = sorted(set(module_only) - set(allowed))
    if unrecorded:
        for osv in unrecorded:
            print(f"govulncheck gate: {osv} in {module_only[osv]} is not recorded", file=sys.stderr)
        fail(f"drop the module or record each new finding in {ALLOWLIST_REL}")

    stale = sorted(set(allowed) - set(module_only))
    if stale:
        for osv in stale:
            print(f"govulncheck gate: {osv} no longer appears", file=sys.stderr)
        fail(f"delete the stale entries from {ALLOWLIST_REL}")

    for osv, module in sorted(module_only.items()):
        recorded = allowed[osv]["module"]
        if module != recorded:
            fail(f"{osv} now reports module {module}; {ALLOWLIST_REL} records {recorded}")

    print(f"govulncheck gate: ok; 0 reachable, {len(module_only)} module-only recorded")
    for osv in sorted(module_only):
        print(f"  {osv} {module_only[osv]} recorded {allowed[osv]['recorded']}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
