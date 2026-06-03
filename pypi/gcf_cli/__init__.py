"""GCF CLI: downloads and runs the prebuilt gcf binary."""

import os
import platform
import stat
import subprocess
import sys
import urllib.request

REPO = "blackwell-systems/gcf-go"
VERSION = "0.1.0"


def get_binary_path():
    """Return path where the binary should be stored."""
    return os.path.join(os.path.dirname(__file__), "bin", _binary_name())


def _binary_name():
    return "gcf.exe" if sys.platform == "win32" else "gcf"


def _platform_suffix():
    system = platform.system().lower()
    machine = platform.machine().lower()

    arch_map = {
        "x86_64": "amd64",
        "amd64": "amd64",
        "aarch64": "arm64",
        "arm64": "arm64",
    }

    os_map = {
        "linux": "linux",
        "darwin": "darwin",
        "windows": "windows",
    }

    os_name = os_map.get(system)
    arch = arch_map.get(machine)

    if not os_name or not arch:
        print(f"Unsupported platform: {system}-{machine}", file=sys.stderr)
        print("Install from source: go install github.com/blackwell-systems/gcf-go/cmd/gcf@latest", file=sys.stderr)
        sys.exit(1)

    suffix = f"{os_name}-{arch}"
    if os_name == "windows":
        suffix += ".exe"
    return suffix


def download_binary():
    """Download the gcf binary for the current platform."""
    binary_path = get_binary_path()
    if os.path.exists(binary_path):
        return binary_path

    os.makedirs(os.path.dirname(binary_path), exist_ok=True)

    suffix = _platform_suffix()
    tag = f"v{VERSION}"
    url = f"https://github.com/{REPO}/releases/download/{tag}/gcf-{suffix}"

    print(f"Downloading gcf {tag} for {suffix}...")

    try:
        urllib.request.urlretrieve(url, binary_path)
        os.chmod(binary_path, os.stat(binary_path).st_mode | stat.S_IEXEC | stat.S_IXGRP | stat.S_IXOTH)
        print(f"Installed gcf to {binary_path}")
    except Exception as e:
        print(f"Failed to download: {e}", file=sys.stderr)
        print(f"URL: {url}", file=sys.stderr)
        print("Install from source: go install github.com/blackwell-systems/gcf-go/cmd/gcf@latest", file=sys.stderr)
        sys.exit(1)

    return binary_path


def main():
    """Entry point: download binary if needed, then exec with args."""
    binary = download_binary()
    result = subprocess.run([binary] + sys.argv[1:])
    sys.exit(result.returncode)
