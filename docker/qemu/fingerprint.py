#!/usr/bin/env python3
"""Hash the merged image filesystem and its runtime configuration."""
import hashlib
import json
import posixpath
import sys
import tarfile
from collections import defaultdict


def filesystem(archive):
    entries = {}
    links = {}
    with tarfile.open(archive, "r|*") as rootfs:
        for entry in rootfs:
            path = posixpath.normpath(entry.name)
            if entry.islnk():
                links[path] = posixpath.normpath(entry.linkname)
                continue
            data = None
            if entry.isfile():
                data = hashlib.file_digest(rootfs.extractfile(entry), "sha256").hexdigest()
            elif entry.issym():
                data = entry.linkname
            elif entry.isdev():
                data = (entry.devmajor, entry.devminor)
            attributes = {k: v for k, v in entry.pax_headers.items()
                          if k.startswith(("SCHILY.xattr.", "LIBARCHIVE.xattr.", "SCHILY.acl."))}
            entries[path] = [entry.type.decode(), entry.mode, entry.uid, entry.gid, attributes, data]

    # All names of a hard-linked file are equivalent, regardless of tar entry order.
    groups = defaultdict(list)
    for path, target in links.items():
        while target in links:
            target = links[target]
        groups[target].append(path)
    for target, aliases in groups.items():
        content = entries.pop(target)
        first, *others = sorted([target, *aliases])
        entries[first] = content
        for path in others:
            entries[path] = ["hardlink", first]
    return entries


def fingerprint(archive, config_path):
    with open(config_path, encoding="utf-8") as config:
        image = json.load(config)
    runtime = {k: image[k] for k in ("os", "architecture", "variant", "os.version", "os.features", "config")
               if k in image}
    # Publishers often put a build date or Git revision in descriptive OCI labels.
    runtime.get("config", {}).pop("Labels", None)
    canonical = json.dumps([runtime, filesystem(archive)], sort_keys=True, separators=(",", ":"))
    return hashlib.sha256(canonical.encode()).hexdigest()


if __name__ == "__main__":
    print(fingerprint(*sys.argv[1:]))
