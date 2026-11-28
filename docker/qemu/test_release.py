import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path


QEMU = Path(__file__).resolve().parent
DIGEST = "sha256:" + "a" * 64
OTHER = "sha256:" + "b" * 64


class PromotionTest(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.command("skopeo", """
case $1 in
    list-tags) jq -c '{Tags: keys}' registry.json ;;
    inspect) jq -er --arg tag "${!#}" '.[$tag | split(":") | last]' registry.json ;;
    *) exit 1 ;;
esac
""")
        self.command("scripts/images/promote.sh", 'printf "%s\\n" "$@" > promoted')

    def command(self, name, script):
        path = self.root / name
        path.parent.mkdir(exist_ok=True, parents=True)
        path.write_text("#!/usr/bin/env bash\nset -euo pipefail\n" + script + "\n")
        path.chmod(0o755)

    def promote(self, registry, expected_tags, success=True):
        (self.root / "registry.json").write_text(json.dumps(registry))
        result = subprocess.run([QEMU / "promote.sh", "ghcr.io/example/qemu", "10.0.13", "2", DIGEST],
                                env={**os.environ, "PATH": f"{self.root}:{os.environ['PATH']}"},
                                cwd=self.root, capture_output=True, text=True)
        self.assertEqual(result.returncode == 0, success, result.stderr)
        record = self.root / "promoted"
        if expected_tags:
            self.assertEqual(record.read_text().splitlines(), ["qemu", DIGEST, *expected_tags])
        else:
            self.assertFalse(record.exists())

    def test_new_revision(self):
        self.promote({"10.0.13-r1": OTHER, "latest": OTHER}, ["10.0.13-r2", "10.0.13", "latest"])

    def test_repair_missing_and_stale_aliases(self):
        self.promote({"10.0.13-r2": DIGEST, "latest": OTHER}, ["10.0.13", "latest"])

    def test_unchanged(self):
        self.promote(dict.fromkeys(["10.0.13-r2", "10.0.13", "latest"], DIGEST), [])

    def test_refuse_occupied_immutable_tag(self):
        self.promote({"10.0.13-r2": OTHER}, [], success=False)

    def test_superseded_revision(self):
        self.promote({"10.0.13-r3": OTHER}, [])

    def test_superseded_upstream_keeps_latest(self):
        self.promote({"10.0.13-r2": DIGEST, "10.0.14-r1": OTHER, "latest": OTHER}, ["10.0.13"])
