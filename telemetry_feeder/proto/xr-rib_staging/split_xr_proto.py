#!/usr/bin/env python3

from __future__ import annotations

import argparse
import pathlib
import re
import shutil
import sys


SYNTAX_RE = re.compile(r'^syntax\s*=\s*"proto3";\s*$')
PACKAGE_RE = re.compile(r"^package\s+([A-Za-z0-9_\.]+);\s*$")
GO_IMPORT_ROOT = "github.com/sbezverk/tools/telemetry_feeder/proto/xr-rib/split"


def sanitize(name: str) -> str:
    return re.sub(r"[^A-Za-z0-9_.-]+", "_", name)


def split_proto(src: pathlib.Path, outdir: pathlib.Path) -> int:
    text = src.read_text(encoding="utf-8")
    lines = text.splitlines(keepends=True)

    starts: list[int] = []
    for idx, line in enumerate(lines):
        if SYNTAX_RE.match(line.strip()):
            starts.append(idx)

    if not starts:
        raise RuntimeError("no proto3 syntax declarations found")

    if outdir.exists():
        shutil.rmtree(outdir)
    outdir.mkdir(parents=True)

    generated = 0
    for i, start in enumerate(starts):
        end = starts[i + 1] if i + 1 < len(starts) else len(lines)
        chunk = "".join(lines[start:end]).lstrip()
        package = None
        for line in chunk.splitlines():
            m = PACKAGE_RE.match(line.strip())
            if m:
                package = m.group(1)
                break
        if package is None:
            raise RuntimeError(f"missing package declaration in chunk {i + 1}")

        package_path = pathlib.Path(*package.split("."))
        package_name = package.split(".")[-1]
        import_path = f"{GO_IMPORT_ROOT}/{package_path.as_posix()}"
        go_package = f'option go_package = "{import_path};{package_name}";\n'
        chunk_lines = chunk.splitlines(keepends=True)
        for idx, line in enumerate(chunk_lines):
            m = PACKAGE_RE.match(line.strip())
            if m:
                chunk_lines.insert(idx + 1, "\n" + go_package)
                break
        chunk = "".join(chunk_lines)

        target_dir = outdir / package_path
        target_dir.mkdir(parents=True, exist_ok=True)
        filename = target_dir / "schema.proto"
        filename.write_text(chunk, encoding="utf-8")
        print(f"{filename.relative_to(outdir)}: {package}")
        generated += 1

    return generated


def main() -> int:
    parser = argparse.ArgumentParser(description="Split XR concatenated proto into individual files")
    parser.add_argument("src", nargs="?", default="xr-rib.proto", help="input proto file")
    parser.add_argument(
        "--outdir",
        default="split",
        help="directory for generated proto fragments",
    )
    args = parser.parse_args()

    src = pathlib.Path(args.src)
    outdir = pathlib.Path(args.outdir)
    count = split_proto(src, outdir)
    print(f"generated {count} proto fragments in {outdir}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
