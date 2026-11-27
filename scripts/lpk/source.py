#!/usr/bin/env python3
"""Generate the MiaoMiao v2 catalog from versioned GitHub Release assets."""

import hashlib
import json
import os
import re
import subprocess
import sys
from pathlib import Path
from urllib.request import urlopen


def build_catalog(base_url, releases):
    versions = []
    for release in releases:
        tag = release["tag_name"]
        match = re.fullmatch(r"lpk-httpbin-v(\d+\.\d+\.\d+)", tag)
        if not match or release["draft"] or release["prerelease"]:
            continue
        version = match[1]
        assets = {asset["name"]: asset for asset in release["assets"]}
        package = assets[f"httpbin-{version}.lpk"]
        with urlopen(assets["release.json"]["browser_download_url"], timeout=60) as response:
            record = json.load(response)
        with urlopen(package["browser_download_url"], timeout=60) as response:
            payload = response.read()
        digest = hashlib.sha256(payload).hexdigest()
        if (record["packageId"] != "com.github.moonfruit.httpbin" or record["version"] != version
                or record["sha256"] != digest or package["digest"] != f"sha256:{digest}"
                or record["size"] != len(payload) or package["size"] != len(payload)):
            raise ValueError(f"Release metadata does not match public download for {tag}")
        entry = dict(version=version, sourceType="LOCAL", downloadUrl=package["browser_download_url"],
                     sha256=digest, size=len(payload))
        versions.append((tuple(map(int, version.split("."))), release["published_at"], entry, record))
    if not versions:
        raise ValueError("No published HttpBin release found")
    versions.sort(reverse=True)
    _, _, latest, metadata = versions[0]
    timestamps = [item[1] for item in versions]
    return {
        "schema": "lazycat.appstore.source.v2", "baseUrl": base_url,
        "site": {"title": "MoonFruit 懒猫软件源", "sourceUrl": f"{base_url}/source/v2/index.json"},
        "categories": [],
        "apps": [{
            "id": 1, "slug": "httpbin", "packageId": metadata["packageId"], "name": metadata["name"],
            "summary": metadata["summary"], "createdAt": min(timestamps), "updatedAt": max(timestamps),
            "latestVersion": latest, "versions": [item[2] for item in versions], "commentsEnabled": False,
        }],
    }


if __name__ == "__main__":
    base_url, output = sys.argv[1:]
    endpoint = f"repos/{os.environ['GITHUB_REPOSITORY']}/releases?per_page=100"
    pages = json.loads(subprocess.check_output(["gh", "api", "--paginate", "--slurp", endpoint]))
    catalog = build_catalog(base_url.rstrip("/"), [release for page in pages for release in page])
    path = Path(output)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(catalog, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
