import io
import json
import tarfile
import tempfile
import unittest
from pathlib import Path

from fingerprint import fingerprint


class FingerprintTest(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)

    def digest(self, entries, command="bash", created=""):
        archive = self.root / "rootfs.tar"
        with tarfile.open(archive, "w") as output:
            for name, data, attrs in entries:
                entry = tarfile.TarInfo(name)
                entry.size = len(data)
                for key, value in attrs.items():
                    setattr(entry, key, value)
                output.addfile(entry, io.BytesIO(data))
        config = self.root / "config.json"
        config.write_text(json.dumps({"created": created, "config": {
            "Cmd": [command], "Labels": {"org.opencontainers.image.created": created}}}))
        return fingerprint(archive, config)

    def test_archive_order_timestamps_and_hardlink_order(self):
        first = [("a", b"same", {}), ("b", b"", {"type": tarfile.LNKTYPE, "linkname": "a"})]
        second = [("a", b"", {"type": tarfile.LNKTYPE, "linkname": "b"}),
                  ("b", b"same", {"mtime": 1000})]
        self.assertEqual(self.digest(first), self.digest(second, created="later"))

    def test_installed_versions_files_and_runtime_configuration(self):
        base = [("var/lib/dpkg/status", b"Version: 1.0-r1", {}), ("etc/example", b"x", {})]
        original = self.digest(base)
        changes = [
            [("var/lib/dpkg/status", b"Version: 1.0-r2", {}), base[1]],
            [base[0], ("etc/example", b"y", {})],
            [base[0], ("etc/example", b"x", {"mode": 0o600})],
            [base[0], ("etc/example", b"x", {"uid": 123})],
            [base[0], ("etc/example", b"x", {"pax_headers": {"SCHILY.xattr.user.test": "1"}})],
            [base[0], ("etc/example", b"", {"type": tarfile.SYMTYPE, "linkname": "target"})],
            base[:1],
            [*base, ("new", b"", {})],
        ]
        for changed in changes:
            with self.subTest(changed=changed):
                self.assertNotEqual(original, self.digest(changed))
        self.assertNotEqual(original, self.digest(base, command="sh"))
