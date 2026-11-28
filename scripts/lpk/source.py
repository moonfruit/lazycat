#!/usr/bin/env python3
"""Generate the MiaoMiao v2 catalog from the registered apps' public Releases."""

import hashlib
import json
import os
import re
import subprocess
import sys
import tarfile
import tempfile
import time
import zipfile
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


def checksum(response, copy=None):
    digest = hashlib.sha256()
    size = 0
    while chunk := response.read(1024 * 1024):
        digest.update(chunk)
        size += len(chunk)
        if copy is not None:
            copy.write(chunk)
    return digest.hexdigest(), size


def package_icon(package):
    package.seek(0)
    # A v2 TAR may itself contain a ZIP near its end (e.g. HAOS's OIDC plugin).
    # Probe the outer TAR header before falling back to the legacy ZIP format.
    try:
        outer = tarfile.open(fileobj=package, mode="r:")
    except tarfile.ReadError:
        package.seek(0)
        with zipfile.ZipFile(package) as archive, archive.open("icon.png") as icon:
            return icon.read()
    with outer as archive:
        member = archive.getmember("icon.png")
        if not member.isfile():
            raise ValueError("Expected a regular icon.png inside the LPK")
        return archive.extractfile(member).read()


def write_icon(directory, base_url, name, png):
    if not png.startswith(b"\x89PNG\r\n\x1a\n"):
        raise ValueError(f"Invalid PNG for {name}")
    filename = f"{name}.png"
    (directory / filename).write_bytes(png)
    revision = hashlib.sha256(png).hexdigest()[:16]
    return f"{base_url}/source/v2/icons/{filename}?v={revision}"


def release_version(release, slug, version, package_id, package_copy=None):
    tag = release["tag_name"]
    assets = {asset["name"]: asset for asset in release["assets"]}
    filename = f"{slug}-{version}.lpk"
    if "release.json" not in assets or filename not in assets:
        raise ValueError(f"{tag} is missing release.json or {filename}")
    package = assets[filename]
    record = download(assets["release.json"]["browser_download_url"], json.load)
    url = package["browser_download_url"]
    def verify_download(response):
        if package_copy is not None:
            package_copy.seek(0)
            package_copy.truncate()
        return checksum(response, package_copy)

    digest, size = download(url, verify_download)
    if (record["packageId"] != package_id or record["version"] != version
            or not isinstance(record["name"], str) or not record["name"].strip()
            or not isinstance(record["summary"], str)
            or record["sha256"] != digest or package["digest"] != f"sha256:{digest}"
            or type(record["size"]) is not int or record["size"] != size or package["size"] != size
            or not size or not release["published_at"]):
        raise ValueError(f"{tag} metadata does not match its registered package or public download")
    entry = dict(version=version, sourceType="LOCAL", downloadUrl=url, sha256=digest, size=size)
    return entry, record, release["published_at"]


def build_catalog(base_url, releases, icon_dir, targets=None):
    if targets is None:
        targets = json.loads(REGISTRY.read_text(encoding="utf-8"))["lpk"]
    apps = []
    icon_dir.mkdir(parents=True, exist_ok=True)
    site_icon = write_icon(icon_dir, base_url, "source", Path(__file__).with_name("source-icon.png").read_bytes())
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
            versions[order] = release, version
        if not versions:
            continue  # Newly registered apps enter the catalog on their first published version.
        latest_order = max(versions)
        ordered = []
        for key in sorted(versions, reverse=True):
            release, version = versions[key]
            if key == latest_order:
                with tempfile.TemporaryFile() as package:
                    ordered.append(release_version(release, slug, version, target["package"], package))
                    icon_url = write_icon(icon_dir, base_url, f"app-{slug}", package_icon(package))
            else:
                ordered.append(release_version(release, slug, version, target["package"]))
        latest, metadata, _ = ordered[0]
        timestamps = [timestamp for _, _, timestamp in ordered]
        apps.append({
            "id": target["id"], "slug": slug, "packageId": target["package"], "name": metadata["name"],
            "summary": metadata["summary"], "createdAt": min(timestamps), "updatedAt": max(timestamps),
            "iconUrl": icon_url,
            "latestVersion": latest, "versions": [entry for entry, _, _ in ordered], "commentsEnabled": False,
        })
    if not apps:
        raise ValueError("No published releases for any registered LPK")
    return {
        "schema": "lazycat.appstore.source.v2", "baseUrl": base_url,
        "site": {"title": "MoonFruit 懒猫软件源", "sourceUrl": f"{base_url}/source/v2/index.json", "iconUrl": site_icon},
        "categories": [], "apps": apps,
    }


if __name__ == "__main__":
    base_url, output = sys.argv[1:]
    endpoint = f"repos/{os.environ['GITHUB_REPOSITORY']}/releases?per_page=100"
    pages = json.loads(subprocess.check_output(["gh", "api", "--paginate", "--slurp", endpoint]))
    path = Path(output)
    catalog = build_catalog(base_url.rstrip("/"), [release for page in pages for release in page], path.parent / "icons")
    path.write_text(json.dumps(catalog, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
