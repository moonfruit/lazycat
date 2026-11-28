#!/usr/bin/env python3
"""Generate the MiaoMiao v2 catalog from the registered apps' public Releases."""

import hashlib
import json
import os
import re
import subprocess
import sys
import time
from http.client import IncompleteRead
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.request import urlopen


REGISTRY = Path(__file__).resolve().parents[2] / ".github/release-targets.json"
VERSION = re.compile(r"[0-9]+\.[0-9]+\.[0-9]+(?:\.[1-9][0-9]*)?")


def download(url, read):
    # A new public Release can be listed before its assets are available on the CDN.
    for attempt in range(4):
        try:
            with urlopen(url, timeout=60) as response:
                return read(response)
        except HTTPError as exc:
            if exc.code not in (404, 408, 429, 500, 502, 503, 504) or attempt == 3:
                raise
        except (URLError, TimeoutError, ConnectionError, IncompleteRead):
            if attempt == 3:
                raise
        time.sleep(2 ** attempt)


def checksum(response):
    digest = hashlib.sha256()
    size = 0
    while chunk := response.read(1024 * 1024):
        digest.update(chunk)
        size += len(chunk)
    return digest.hexdigest(), size


def release_version(release, slug, version, package_id):
    tag = release["tag_name"]
    assets = {asset["name"]: asset for asset in release["assets"]}
    filename = f"{slug}-{version}.lpk"
    if "release.json" not in assets or filename not in assets:
        raise ValueError(f"{tag} is missing release.json or {filename}")
    package = assets[filename]
    record = download(assets["release.json"]["browser_download_url"], json.load)
    url = package["browser_download_url"]
    digest, size = download(url, checksum)
    if (record["packageId"] != package_id or record["version"] != version
            or not isinstance(record["name"], str) or not record["name"].strip()
            or not isinstance(record["summary"], str)
            or record["sha256"] != digest or package["digest"] != f"sha256:{digest}"
            or type(record["size"]) is not int or record["size"] != size or package["size"] != size
            or not size or not release["published_at"]):
        raise ValueError(f"{tag} metadata does not match its registered package or public download")
    entry = dict(version=version, sourceType="LOCAL", downloadUrl=url, sha256=digest, size=size)
    return entry, record, release["published_at"]


def build_catalog(base_url, releases, targets=None):
    if targets is None:
        targets = json.loads(REGISTRY.read_text(encoding="utf-8"))["lpk"]
    apps = []
    ids = [target["id"] for target in targets.values()]
    packages = [target["package"] for target in targets.values()]
    if (any(type(app_id) is not int or app_id < 1 for app_id in ids)
            or len(set(ids)) != len(ids) or len(set(packages)) != len(packages)):
        raise ValueError("LPK registrations need unique positive app IDs and unique package IDs")
    for slug, target in sorted(targets.items(), key=lambda item: item[1]["id"]):
        versions = {}
        for release in releases:
            tag = release["tag_name"]
            label, _, version = tag.rpartition("-v")
            if release["draft"] or release["prerelease"] or label != f"lpk-{slug}":
                continue
            if not VERSION.fullmatch(version):
                raise ValueError(f"Invalid registered LPK release tag: {tag}")
            order = tuple(map(int, version.split(".")))
            if order in versions:
                raise ValueError(f"Duplicate version for {tag}")
            versions[order] = release_version(release, slug, version, target["package"])
        if not versions:
            continue  # Newly registered apps enter the catalog on their first published version.
        ordered = [versions[key] for key in sorted(versions, reverse=True)]
        latest, metadata, _ = ordered[0]
        timestamps = [timestamp for _, _, timestamp in ordered]
        apps.append({
            "id": target["id"], "slug": slug, "packageId": target["package"], "name": metadata["name"],
            "summary": metadata["summary"], "createdAt": min(timestamps), "updatedAt": max(timestamps),
            "latestVersion": latest, "versions": [entry for entry, _, _ in ordered], "commentsEnabled": False,
        })
    if not apps:
        raise ValueError("No published releases for any registered LPK")
    return {
        "schema": "lazycat.appstore.source.v2", "baseUrl": base_url,
        "site": {"title": "MoonFruit 懒猫软件源", "sourceUrl": f"{base_url}/source/v2/index.json"},
        "categories": [], "apps": apps,
    }


if __name__ == "__main__":
    base_url, output = sys.argv[1:]
    endpoint = f"repos/{os.environ['GITHUB_REPOSITORY']}/releases?per_page=100"
    pages = json.loads(subprocess.check_output(["gh", "api", "--paginate", "--slurp", endpoint]))
    catalog = build_catalog(base_url.rstrip("/"), [release for page in pages for release in page])
    path = Path(output)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(catalog, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
