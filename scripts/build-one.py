#!/usr/bin/env python3
from __future__ import annotations

import subprocess
import sys
from pathlib import Path


class Fatal(Exception):
    pass


def _usage() -> str:
    return "usage: scripts/build-one.py <image-dir-name> [tag]\nexample: scripts/build-one.py kube-tools dev"


def _load_build_args(path: Path) -> list[str]:
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

    build_args = data.get("build_args", {})
    if not isinstance(build_args, dict):
        raise Fatal(f"{path}: build_args must be a table of string keys and values")
    if not all(isinstance(k, str) and isinstance(v, str) for k, v in build_args.items()):
        raise Fatal(f"{path}: build_args must be a table of string keys and values")

    return [f"{k}={v}" for k, v in build_args.items()]


def main(argv: list[str]) -> int:
    if not argv:
        sys.stderr.write(f"{_usage()}\n")
        return 2

    image = argv[0]
    tag = argv[1] if len(argv) > 1 else "dev"

    script_dir = Path(__file__).resolve().parent
    repo_root = script_dir.parent
    image_dir = repo_root / "images" / image
    dockerfile = image_dir / "Dockerfile"
    meta = image_dir / "image.toml"

    if not dockerfile.is_file():
        raise Fatal(f"missing {dockerfile}")
    if not meta.is_file():
        raise Fatal(f"missing {meta}")

    build_args = _load_build_args(meta)

    cmd = ["docker", "buildx", "build", "--load", "-t", f"local/{image}:{tag}"]
    for arg in build_args:
        cmd.extend(["--build-arg", arg])
    cmd.append(str(image_dir))

    p = subprocess.run(cmd, cwd=repo_root)
    return p.returncode


if __name__ == "__main__":
    try:
        raise SystemExit(main(sys.argv[1:]))
    except Fatal as e:
        sys.stderr.write(f"error: {e}\n")
        raise SystemExit(2)
