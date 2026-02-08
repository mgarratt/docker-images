#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
IMAGES_DIR = ROOT / "images"
DEFAULT_PLATFORMS = ["linux/amd64", "linux/arm64"]


class Fatal(Exception):
    pass


def _run(cmd: list[str]) -> str:
    p = subprocess.run(cmd, cwd=ROOT, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if p.returncode != 0:
        raise Fatal(f"command failed: {' '.join(cmd)}\n{p.stderr.strip()}")
    return p.stdout


def _git_commit_exists(ref: str) -> bool:
    p = subprocess.run(
        ["git", "rev-parse", "--verify", "--quiet", f"{ref}^{{commit}}"],
        cwd=ROOT,
        text=True,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    return p.returncode == 0


def _git_has_merge_base(base: str, head: str) -> bool:
    p = subprocess.run(
        ["git", "merge-base", base, head],
        cwd=ROOT,
        text=True,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    return p.returncode == 0


def _git_diff_names(base: str, head: str) -> list[str]:
    out = _run(["git", "diff", "--name-only", f"{base}..{head}"])
    return [line.strip() for line in out.splitlines() if line.strip()]


def _is_all_zeros_sha(s: str) -> bool:
    s = s.strip()
    return len(s) >= 7 and set(s) == {"0"}


def _load_image_toml(path: Path) -> dict:
    try:
        import tomllib  # py311+
    except ModuleNotFoundError as e:  # pragma: no cover
        raise Fatal("python tomllib not available; require python >= 3.11") from e

    try:
        data = tomllib.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError:
        raise Fatal(f"missing metadata: {path}")
    except Exception as e:
        raise Fatal(f"failed parsing {path}: {e}") from e

    allowed = {"image", "version", "platforms", "build_args"}
    unknown = set(data.keys()) - allowed
    if unknown:
        raise Fatal(f"unknown keys in {path}: {', '.join(sorted(unknown))}")

    if "platforms" in data and not isinstance(data["platforms"], list):
        raise Fatal(f"{path}: platforms must be an array of strings")
    if "version" in data and not isinstance(data["version"], str):
        raise Fatal(f"{path}: version must be a string")
    if "image" in data and not isinstance(data["image"], str):
        raise Fatal(f"{path}: image must be a string")
    if "build_args" in data:
        if not isinstance(data["build_args"], dict):
            raise Fatal(f"{path}: build_args must be a table of string keys and values")
        if not all(isinstance(k, str) and isinstance(v, str) for k, v in data["build_args"].items()):
            raise Fatal(f"{path}: build_args must be a table of string keys and values")

    return data


def discover_images() -> list[dict]:
    if not IMAGES_DIR.is_dir():
        return []

    images: list[dict] = []
    for p in sorted(IMAGES_DIR.iterdir()):
        if not p.is_dir():
            continue
        dockerfile = p / "Dockerfile"
        meta = p / "image.toml"
        if not dockerfile.is_file() or not meta.is_file():
            continue

        data = _load_image_toml(meta)
        image_name = data.get("image") or p.name
        version = data.get("version")
        platforms = data.get("platforms") or DEFAULT_PLATFORMS
        build_args = [f"{k}={v}" for k, v in data.get("build_args", {}).items()]

        if not all(isinstance(x, str) for x in platforms):
            raise Fatal(f"{meta}: platforms must be an array of strings")

        images.append(
            {
                "dir": str(p.relative_to(ROOT)),
                "image_name": image_name,
                "version": version,
                "platforms": platforms,
                "build_args": build_args,
            }
        )
    return images


def changed_images(base: str | None, head: str) -> list[dict]:
    all_images = discover_images()
    by_dir = {img["dir"]: img for img in all_images}

    if not base or _is_all_zeros_sha(base):
        return all_images

    # Force-pushes and shallow history can make GitHub-provided refs unusable
    # in CI. Prefer rebuilding all images over failing the workflow.
    if not _git_commit_exists(base) or not _git_commit_exists(head):
        return all_images
    if not _git_has_merge_base(base, head):
        return all_images

    changed_files = _git_diff_names(base, head)

    # Rebuild everything if toolchain or build scripts changed.
    if any(
        f == "mise.toml"
        or f.startswith("scripts/")
        for f in changed_files
    ):
        return all_images

    dirs: set[str] = set()
    for f in changed_files:
        if not f.startswith("images/"):
            continue
        parts = f.split("/", 2)
        if len(parts) < 2:
            continue
        d = "/".join(parts[:2])
        if d in by_dir:
            dirs.add(d)

    return [by_dir[d] for d in sorted(dirs)]


def main(argv: list[str]) -> int:
    ap = argparse.ArgumentParser(description="Generate GitHub Actions build matrix for images/")
    sub = ap.add_subparsers(dest="cmd", required=True)

    ap_all = sub.add_parser("all", help="Matrix for all images")
    ap_all.add_argument("--json", action="store_true", help="Print JSON matrix")

    ap_changed = sub.add_parser("changed", help="Matrix for changed images (git diff)")
    ap_changed.add_argument("--base", required=False, help="Base ref/SHA (if omitted or zeros, builds all)")
    ap_changed.add_argument("--head", default="HEAD", help="Head ref/SHA (default: HEAD)")
    ap_changed.add_argument("--json", action="store_true", help="Print JSON matrix")

    args = ap.parse_args(argv)

    if args.cmd == "all":
        imgs = discover_images()
    else:
        imgs = changed_images(args.base, args.head)

    matrix = {"include": imgs}
    if args.json:
        sys.stdout.write(json.dumps(matrix, indent=2, sort_keys=True))
        sys.stdout.write("\n")
        return 0

    # Human-friendly output
    for img in imgs:
        v = img["version"] or "-"
        sys.stdout.write(f"{img['dir']} image={img['image_name']} version={v}\n")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main(sys.argv[1:]))
    except Fatal as e:
        sys.stderr.write(f"error: {e}\n")
        raise SystemExit(2)
