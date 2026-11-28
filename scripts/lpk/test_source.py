import io
import tarfile
import unittest
import zipfile

from source import package_icon


def make_zip(name, data):
    output = io.BytesIO()
    with zipfile.ZipFile(output, "w") as archive:
        archive.writestr(name, data)
    return output.getvalue()


class PackageIconTests(unittest.TestCase):
    def test_legacy_zip(self):
        self.assertEqual(package_icon(io.BytesIO(make_zip("icon.png", b"legacy"))), b"legacy")

    def test_tar_with_embedded_zip(self):
        package = io.BytesIO()
        with tarfile.open(fileobj=package, mode="w") as archive:
            for name, data in [("icon.png", b"outer icon"), ("plugin.zip", make_zip("manifest.json", b"{}"))]:
                member = tarfile.TarInfo(name)
                member.size = len(data)
                archive.addfile(member, io.BytesIO(data))
        self.assertTrue(zipfile.is_zipfile(package))  # The embedded ZIP is visible from the end.
        self.assertEqual(package_icon(package), b"outer icon")
