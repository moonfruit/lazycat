#!/usr/bin/env python3
"""Refresh the Alpine base and x86_64 APK version pins in the current Dockerfile."""

import io
import json
import re
import subprocess
import sys
import tarfile
from functools import lru_cache
from pathlib import Path


def latest_alpine():
    tags = json.loads(subprocess.check_output(["skopeo", "list-tags", "docker://docker.io/library/alpine"]))
    stable = (tag for tag in tags["Tags"] if re.fullmatch(r"\d+\.\d+\.\d+", tag))
    return max(stable, key=lambda tag: tuple(map(int, tag.split("."))))


@lru_cache(maxsize=None)
def package_index(branch, repository):
    url = f"https://dl-cdn.alpinelinux.org/alpine/v{branch}/{repository}/x86_64/APKINDEX.tar.gz"
    data = subprocess.check_output(["curl", "-fsSL", "--retry", "3", url])
    # Signed APK indexes contain concatenated tar archives.
    with tarfile.open(fileobj=io.BytesIO(data), ignore_zeros=True) as archive:
        return archive.extractfile("APKINDEX").read().decode()


def replace_pin(text, pattern, pin):
    updated, count = re.subn(pattern, lambda match: match[1] + pin, text, flags=re.MULTILINE)
    if count != 1:
        raise ValueError(f"Expected one Dockerfile match for {pattern!r}, found {count}")
    return updated


def main():
    path = Path("Dockerfile")
    original = path.read_text()
    alpine = latest_alpine()
    branch = alpine.rsplit(".", 1)[0]
    print(f"Alpine {alpine}")
    updated = replace_pin(original, r"^(FROM alpine:)\S+", alpine)
    for spec in sys.argv[1:]:
        key, source = spec.split("=")
        repository, package = source.split("/")
        index = package_index(branch, repository)
        versions = re.findall(rf"^P:{re.escape(package)}\nV:([^\n]+)$", index, flags=re.MULTILINE)
        if len(versions) != 1 or not re.fullmatch(r"[0-9][0-9A-Za-z._+-]*", versions[0]):
            raise ValueError(f"Expected one version of {package} in v{branch}/{repository}, found {versions}")
        print(f"{package} {versions[0]}")
        updated = replace_pin(updated, rf"^((?:ENV |[ \t]+){re.escape(key)}=)\S+", versions[0])
    if updated != original:
        path.write_text(updated)


if __name__ == "__main__":
    main()
