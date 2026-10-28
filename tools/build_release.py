# Builds the four release binaries and checks that each one really is a
# program for its system — a "-linux" file that is secretly a Windows .exe
# (GOOS not applied by the shell) runs nowhere and only says "Exec format
# error" on Linux. Then packs the Linux and Windows archives.
#
#   python tools/build_release.py            # all four + dayz-manager-linux.tar.gz + dayz-manager-windows.zip
#   python tools/build_release.py linux      # only the Linux builds + archive
#   python tools/build_release.py windows    # only the Windows builds + archive
#
# Works the same from cmd, PowerShell or bash: the target system is passed in
# the environment of each go build, never through shell syntax.
import os
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

# (output file, package, GOOS, extra -ldflags)
BUILDS = [
    ("dayz-manager-web.exe", "./cmd/manager", "windows", ""),
    ("dayz-manager-panel.exe", "./cmd/panel", "windows", "-H windowsgui"),
    ("dayz-manager-web-linux", "./cmd/manager", "linux", ""),
    ("dayz-manager-panel-linux", "./cmd/panel", "linux", ""),
]

MAGIC = {"windows": b"MZ", "linux": b"\x7fELF"}


def build(out, pkg, goos, ldextra):
    env = dict(os.environ, GOOS=goos, GOARCH="amd64", CGO_ENABLED="0")
    ldflags = ("-s -w " + ldextra).strip()
    cmd = ["go", "build", "-trimpath", "-ldflags=" + ldflags, "-o", out, pkg]
    print("  %-26s GOOS=%s  %s" % (out, goos, " ".join(cmd[2:])))
    subprocess.run(cmd, cwd=ROOT, env=env, check=True)
    with open(os.path.join(ROOT, out), "rb") as f:
        head = f.read(4)
    if not head.startswith(MAGIC[goos]):
        sys.exit("ERROR: %s is not a %s program (starts with %r) — the build target was not applied" % (out, goos, head))
    size = os.path.getsize(os.path.join(ROOT, out))
    print("      ok: %s binary, %.1f MB" % (goos, size / 1048576))


def main():
    only = sys.argv[1] if len(sys.argv) > 1 else ""
    if only not in ("", "linux", "windows"):
        sys.exit("usage: build_release.py [linux|windows]")
    print("Building release binaries:")
    for out, pkg, goos, ld in BUILDS:
        if only and goos != only:
            continue
        build(out, pkg, goos, ld)
    if only in ("", "linux"):
        subprocess.run([sys.executable, os.path.join(ROOT, "tools", "make_linux_tarball.py")], cwd=ROOT, check=True)
    if only in ("", "windows"):
        subprocess.run([sys.executable, os.path.join(ROOT, "tools", "make_windows_zip.py")], cwd=ROOT, check=True)
    print("Done.")


if __name__ == "__main__":
    main()
