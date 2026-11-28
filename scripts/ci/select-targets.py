#!/usr/bin/env python3
"""Select registered release targets for a push, daily check or manual run."""

import json
import os
import re
import subprocess
import sys
from pathlib import Path


REGISTRY = ".github/release-targets.json"
SHARED = {
    "lpk": (".github/workflows/publish-lpks.yml", ".github/workflows/verify-lpk.yml", "scripts/lpk/"),
    "images": (".github/workflows/build-images.yml", "scripts/images/"),
}


def select(kind, event, target, before):
    entries = json.loads(Path(REGISTRY).read_text())[kind]
    if not all(re.fullmatch(r"[a-z0-9]+(?:-[a-z0-9]+)*", key) for key in entries):
        raise ValueError(f"Invalid slug in {kind} registry")
    if kind == "lpk":
        ids = [entry["id"] for entry in entries.values()]
        packages = [entry["package"] for entry in entries.values()]
        if len(set(ids)) != len(ids) or len(set(packages)) != len(packages):
            raise ValueError("Each registered LPK must have a unique catalog ID and package ID")
    if target != "all":
        if target not in entries:
            raise ValueError(f"Unknown {kind} target: {target}")
        return [target]
    if event != "push" or not before or not before.strip("0"):
        return list(entries)

    # A force push may leave the old commit unreachable. Reconcile all registered targets in that case.
    if subprocess.run(["git", "cat-file", "-e", f"{before}^{{commit}}"], stderr=subprocess.DEVNULL).returncode:
        return list(entries)
    changed = subprocess.check_output(["git", "diff", "--no-renames", "--name-only", "-z", before, "HEAD"])
    paths = os.fsdecode(changed).split("\0")
    shared = ("scripts/ci/select-targets.py", *SHARED[kind])
    if any(path == item or (item.endswith("/") and path.startswith(item)) for path in paths for item in shared):
        return list(entries)
    previous = entries
    if REGISTRY in paths:
        previous = {}
        if subprocess.check_output(["git", "ls-tree", "--name-only", before, "--", REGISTRY]):
            previous = json.loads(subprocess.check_output(["git", "show", f"{before}:{REGISTRY}"]))[kind]
        if kind == "lpk" and previous.keys() - entries.keys():
            return list(entries)  # Rebuild the catalog after withdrawing an app.
    prefix = "docker/" if kind == "images" else ""
    return [slug for slug, config in entries.items()
            if config != previous.get(slug) or any(path.startswith(f"{prefix}{slug}/") for path in paths)]


if __name__ == "__main__":
    kind, = sys.argv[1:]
    print(json.dumps(select(kind, os.environ["EVENT"], os.environ.get("TARGET") or "all",
                            os.environ.get("BEFORE")), separators=(",", ":")))
