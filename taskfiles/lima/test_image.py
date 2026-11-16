"""Tests for the image and pull tasks of the lima Taskfile.

Each test copies the Taskfiles of the repository to a temporary
directory and runs the tasks there with go-task. Fakes of nix, zstd,
and oras stand in for the real commands, so no test builds an image or
reaches the network. HOME names a temporary directory, so no test reads
or writes ~/.lima.

Run with: python3 test_image.py. The tests skip when task is not on
PATH.
"""

import os
import re
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
TASK = shutil.which("task")

# Each fake appends its command line to $FAKE_DIR/calls, and fails when
# the file $FAKE_DIR/<name>-fails exists.
#
# The file $FAKE_DIR/store-path names the store path that the fake flake
# evaluates the image to. `nix eval` prints that path. `nix build`
# creates the path with a read-only image in it, as the Nix store holds
# it, and points the out-link at the path.
FAKE_NIX = """\
#!/bin/sh
echo "nix $*" >>"$FAKE_DIR/calls"
if [ -e "$FAKE_DIR/nix-$1-fails" ]; then
  exit 1
fi
out=$(cat "$FAKE_DIR/store-path")
case "$1" in
eval)
  printf %s "$out"
  ;;
build)
  if [ ! -e "$out" ]; then
    mkdir -p "$out"
    echo "image of $out" >"$out/nixos.qcow2"
    chmod a-w "$out/nixos.qcow2"
  fi
  while [ $# -gt 0 ] && [ "$1" != --out-link ]; do
    shift
  done
  ln -sfn "$out" "$2"
  ;;
*)
  exit 2
  ;;
esac
"""

# The fake zstd copies its input to the file that -o names, for a
# compression and for a decompression.
FAKE_ZSTD = """\
#!/bin/sh
echo "zstd $*" >>"$FAKE_DIR/calls"
if [ -e "$FAKE_DIR/zstd-fails" ]; then
  exit 1
fi
src=
dst=
while [ $# -gt 0 ]; do
  case "$1" in
  -o)
    dst=$2
    shift
    ;;
  -*) ;;
  *) src=$1 ;;
  esac
  shift
done
cp -f "$src" "$dst"
"""

# The fake oras writes a compressed image to the directory that -o names.
FAKE_ORAS = """\
#!/bin/sh
echo "oras $*" >>"$FAKE_DIR/calls"
while [ $# -gt 0 ] && [ "$1" != -o ]; do
  shift
done
echo "pulled image" >"$2/terrarium.qcow2.zst"
"""

FAKES = {"nix": FAKE_NIX, "zstd": FAKE_ZSTD, "oras": FAKE_ORAS}


@unittest.skipUnless(TASK, "task is not on PATH")
class ImageTaskTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        root = Path(tmp.name)
        self.repo = root / "repo"
        self.fake = root / "fake"
        self.store = root / "store"
        home = root / "home"
        bin_dir = root / "bin"
        images = home / ".lima" / "_images"
        self.image = images / "terrarium.qcow2"
        self.compressed = images / "terrarium.qcow2.zst"
        self.record = images / "terrarium.store-path"

        for path in (self.repo / "lima", self.fake, home, bin_dir):
            path.mkdir(parents=True)
        shutil.copy(REPO / "Taskfile.yaml", self.repo)
        shutil.copytree(REPO / "taskfiles", self.repo / "taskfiles")
        for name, text in FAKES.items():
            (bin_dir / name).write_text(text)
            (bin_dir / name).chmod(0o755)
        self.env = {
            **os.environ,
            "HOME": str(home),
            "FAKE_DIR": str(self.fake),
            "PATH": f"{bin_dir}{os.pathsep}{os.environ['PATH']}",
        }

    def evaluate_to(self, name):
        """Make the fake flake evaluate the image to a store path, and return the path.

        An edit to any file or flake input that the image depends on gives
        the image of the real flake a new store path.
        """
        path = self.store / f"{name}-nixos-disk-image"
        (self.fake / "store-path").write_text(f"{path}\n")
        return path

    def make_fail(self, command):
        """Make a fake command fail from now on, such as nix-build or zstd."""
        (self.fake / f"{command}-fails").touch()

    def task(self, *args):
        """Run task in the copy of the repository and return its process."""
        return subprocess.run(
            [TASK, *args],
            cwd=self.repo,
            env=self.env,
            check=False,
            capture_output=True,
            text=True,
        )

    def run_task(self, name):
        """Run one task that must pass."""
        proc = self.task(name)
        self.assertEqual(proc.returncode, 0, proc.stderr)

    def up_to_date(self):
        """Report whether task finds the image task up to date."""
        proc = self.task("--status", "lima:image")
        if proc.returncode != 0:
            self.assertIn("is not up-to-date", proc.stderr)
        return proc.returncode == 0

    def calls(self, prefix):
        """Return the command lines of the fakes that start with the prefix."""
        log = self.fake / "calls"
        if not log.exists():
            return []
        return [
            line for line in log.read_text().splitlines() if line.startswith(prefix)
        ]

    def test_first_run_builds_the_image(self):
        one = self.evaluate_to("one")
        self.assertFalse(self.up_to_date())
        self.run_task("lima:image")
        self.assertEqual(len(self.calls("nix build")), 1)
        self.assertEqual(self.image.read_text(), f"image of {one}\n")
        self.assertEqual(self.compressed.read_text(), f"image of {one}\n")
        self.assertEqual(self.record.read_text(), f"{one}\n")
        self.assertEqual(list((self.repo / "lima").iterdir()), [])

    def test_unchanged_store_path_is_up_to_date(self):
        self.evaluate_to("one")
        self.run_task("lima:image")
        self.assertTrue(self.up_to_date())
        self.run_task("lima:image")
        self.assertEqual(len(self.calls("nix build")), 1)
        self.assertEqual(len(self.calls("zstd")), 1)

    def test_new_store_path_builds_again(self):
        self.evaluate_to("one")
        self.run_task("lima:image")
        two = self.evaluate_to("two")
        self.assertFalse(self.up_to_date())
        self.run_task("lima:image")
        self.assertEqual(len(self.calls("nix build")), 2)
        self.assertEqual(self.image.read_text(), f"image of {two}\n")
        self.assertEqual(self.compressed.read_text(), f"image of {two}\n")
        self.assertEqual(self.record.read_text(), f"{two}\n")
        self.assertTrue(self.up_to_date())

    def test_missing_file_builds_again(self):
        self.evaluate_to("one")
        for path in (self.image, self.compressed, self.record):
            with self.subTest(path=path.name):
                self.run_task("lima:image")
                path.unlink()
                self.assertFalse(self.up_to_date())
                self.run_task("lima:image")
                self.assertTrue(path.exists())
                self.assertTrue(self.up_to_date())

    def test_failed_evaluation_is_not_up_to_date(self):
        self.evaluate_to("one")
        self.run_task("lima:image")
        self.make_fail("nix-eval")
        self.assertFalse(self.up_to_date())
        # A failed evaluation prints no store path, and an empty record
        # holds none.
        self.record.write_text("")
        self.assertFalse(self.up_to_date())

    def test_failed_build_keeps_the_old_image(self):
        one = self.evaluate_to("one")
        self.run_task("lima:image")
        self.evaluate_to("two")
        self.make_fail("nix-build")
        self.assertNotEqual(self.task("lima:image").returncode, 0)
        self.assertEqual(self.image.read_text(), f"image of {one}\n")
        self.assertEqual(self.record.read_text(), f"{one}\n")
        self.evaluate_to("one")
        self.assertTrue(self.up_to_date())

    def test_failed_compression_leaves_no_record(self):
        self.evaluate_to("one")
        self.run_task("lima:image")
        two = self.evaluate_to("two")
        self.make_fail("zstd")
        self.assertNotEqual(self.task("lima:image").returncode, 0)
        self.assertEqual(self.image.read_text(), f"image of {two}\n")
        self.assertFalse(self.record.exists())
        # The image of the second store path replaced the first image, so
        # the first store path is stale as well.
        self.evaluate_to("one")
        self.assertFalse(self.up_to_date())

    def test_status_evaluates_the_package_that_the_task_builds(self):
        self.evaluate_to("one")
        self.run_task("lima:image")
        # The first run has no image files, so its status check stops
        # before the evaluation.
        self.assertEqual(self.calls("nix eval"), [])
        self.assertTrue(self.up_to_date())
        (build,) = self.calls("nix build")
        (evaluation,) = self.calls("nix eval")
        attr = re.search(r"\.#packages\.\S+-linux\.terrarium-image(?= |$)", build)
        self.assertIsNotNone(attr, build)
        self.assertIn(f" {attr.group()}.outPath", evaluation)

    def test_pull_drops_the_record(self):
        one = self.evaluate_to("one")
        self.run_task("lima:image")
        self.image.unlink()
        self.run_task("lima:pull")
        self.assertEqual(self.image.read_text(), "pulled image\n")
        self.assertFalse(self.record.exists())
        self.assertFalse(self.up_to_date())
        self.run_task("lima:image")
        self.assertEqual(self.image.read_text(), f"image of {one}\n")


if __name__ == "__main__":
    unittest.main()
