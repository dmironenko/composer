import hashlib
import io
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().parents[2] / "install.sh"
VERSION = "v1.0.0"


class InstallTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="composer install ")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.assets = self.root / "assets"
        self.assets.mkdir()
        self.tools = self.root / "tools"
        self.tools.mkdir()
        self.destination = self.root / "bin with spaces"
        self.destination.mkdir()
        self.binary = self.destination / "composer"
        self.binary.write_bytes(b"previous installation")
        self.env = dict(os.environ, HOME=str(self.root), TMPDIR=str(self.root),
                        PATH=str(self.tools) + os.pathsep + os.environ["PATH"],
                        TEST_ASSETS=str(self.assets), TEST_OS="Linux", TEST_ARCH="x86_64")
        self.write_tool("uname", '#!/bin/sh\ncase "$1" in -s) echo "$TEST_OS";; -m) echo "$TEST_ARCH";; esac\n')
        self.write_tool("curl", f"#!{sys.executable}\n" + '''import os, pathlib, shutil, sys
args = sys.argv[1:]
url = next(a for a in args if a.startswith("https://"))
assert url.startswith("https://github.com/dmironenko/composer/releases/download/v1.0.0/")
source = pathlib.Path(os.environ["TEST_ASSETS"]) / url.rsplit("/", 1)[1]
if not source.is_file():
    sys.exit(22)
shutil.copyfile(source, args[args.index("-o") + 1])
''')
        self.make_archive()

    def write_tool(self, name, content):
        path = self.tools / name
        path.write_text(content)
        path.chmod(0o755)

    def make_archive(self, platform="linux_amd64", version=VERSION, symlink=False):
        self.archive = self.assets / f"composer_{VERSION}_{platform}.tar.gz"
        content = f"#!/bin/sh\nprintf 'composer version {version}\\n'\n".encode()
        with tarfile.open(self.archive, "w:gz") as archive:
            entry = tarfile.TarInfo("composer")
            entry.mode = 0o755
            if symlink:
                entry.type = tarfile.SYMTYPE
                entry.linkname = str(self.binary)
                archive.addfile(entry)
            else:
                entry.size = len(content)
                archive.addfile(entry, io.BytesIO(content))
        checksum = hashlib.sha256(self.archive.read_bytes()).hexdigest()
        (self.assets / "checksums.txt").write_text(f"{checksum}  ./{self.archive.name}\n")

    def run_installer(self, *args):
        return subprocess.run(["/bin/sh", str(SCRIPT), *args], env=self.env,
                              cwd=self.root, text=True, capture_output=True)

    def assert_preserved(self, result):
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertEqual(self.binary.read_bytes(), b"previous installation")
        self.assertEqual(list(self.destination.iterdir()), [self.binary])
        self.assertEqual(set(p.name for p in self.root.iterdir()),
                         {"assets", "tools", "bin with spaces"})

    def test_platforms_and_atomic_update(self):
        for system, machine, platform in [("Linux", "x86_64", "linux_amd64"),
                                          ("Linux", "aarch64", "linux_arm64"),
                                          ("Darwin", "x86_64", "darwin_amd64"),
                                          ("Darwin", "arm64", "darwin_arm64")]:
            with self.subTest(platform=platform):
                self.env.update(TEST_OS=system, TEST_ARCH=machine)
                self.make_archive(platform)
                with self.binary.open("rb") as old:
                    result = self.run_installer(VERSION, str(self.destination))
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertEqual(old.read(), b"previous installation")
                self.assertEqual(subprocess.check_output([str(self.binary), "--version"], text=True).strip(),
                                 f"composer version {VERSION}")
                self.binary.write_bytes(b"previous installation")

    def test_default_user_directory(self):
        result = self.run_installer(VERSION)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue((self.root / ".local/bin/composer").is_file())
        self.assertIn("PATH", result.stdout)
        self.assertEqual(self.binary.read_bytes(), b"previous installation")

    def test_checksum_failure(self):
        self.archive.write_bytes(b"corrupt download")
        self.assert_preserved(self.run_installer(VERSION, str(self.destination)))

    def test_missing_checksum(self):
        (self.assets / "checksums.txt").write_text("")
        self.assert_preserved(self.run_installer(VERSION, str(self.destination)))

    def test_download_failure(self):
        self.archive.unlink()
        self.assert_preserved(self.run_installer(VERSION, str(self.destination)))

    def test_wrong_binary_version(self):
        self.make_archive(version="v0.0.0")
        self.assert_preserved(self.run_installer(VERSION, str(self.destination)))

    def test_symlink_in_archive(self):
        self.make_archive(symlink=True)
        self.assert_preserved(self.run_installer(VERSION, str(self.destination)))

    def test_install_failure(self):
        self.write_tool("install", "#!/bin/sh\nexit 1\n")
        self.assert_preserved(self.run_installer(VERSION, str(self.destination)))

    def test_existing_directory(self):
        self.binary.unlink()
        self.binary.mkdir()
        result = self.run_installer(VERSION, str(self.destination))
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(list(self.binary.iterdir()), [])

    def test_existing_symlink_does_not_overwrite_its_target(self):
        original = self.root / "original"
        self.binary.rename(original)
        self.binary.symlink_to(original)
        result = self.run_installer(VERSION, str(self.destination))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(self.binary.is_symlink())
        self.assertEqual(original.read_bytes(), b"previous installation")

    def test_incomplete_piped_script(self):
        partial = SCRIPT.read_text().split('  mkdir -p "$install_dir"')[0]
        result = subprocess.run(["/bin/sh", "-s", "--", VERSION, str(self.destination)],
                                input=partial, env=self.env, text=True, capture_output=True)
        self.assert_preserved(result)

    def test_unsupported_platform(self):
        for system, machine in [("FreeBSD", "x86_64"), ("Linux", "armv7l")]:
            with self.subTest(system=system, machine=machine):
                self.env.update(TEST_OS=system, TEST_ARCH=machine)
                self.assert_preserved(self.run_installer(VERSION, str(self.destination)))

    def test_invalid_arguments(self):
        for args in [[], ["../../invalid"], ["v1.0.0;false"], [VERSION, "/tmp", "extra"]]:
            with self.subTest(args=args):
                self.assert_preserved(self.run_installer(*args))

    def test_shasum_fallback(self):
        for name in ["awk", "tar", "gzip", "mktemp", "rm", "mkdir", "install", "mv", "shasum"]:
            (self.tools / name).symlink_to(shutil.which(name))
        self.env["PATH"] = str(self.tools)
        result = self.run_installer(VERSION, str(self.destination))
        self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    unittest.main()
