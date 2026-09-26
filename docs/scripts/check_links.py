#!/usr/bin/env python3
"""Check that every relative link in the repo's Markdown files resolves.

    python3 docs/scripts/check_links.py              # whole repo
    python3 docs/scripts/check_links.py docs README.md
    python3 docs/scripts/check_links.py --no-anchors  # only check that files exist

Links with a scheme (http, https, mailto, ...) are skipped. `path#anchor` links into a
Markdown file also check that the heading exists (GitHub's slug rules). Fenced code blocks
and inline code are ignored. Exit status is 1 when anything is broken. Standard library only.
"""

from __future__ import annotations

import argparse
import os
import re
import sys
import unicodedata
from pathlib import Path
from urllib.parse import unquote

SKIP_DIRS = {
    ".git", ".venv", "venv", "node_modules", "DerivedData", ".idea", ".cache", "__pycache__",
    "xcuserdata", ".mongo-data", "out", "wandb",
}
SCHEME = re.compile(r"^[a-zA-Z][a-zA-Z0-9+.\-]*:")
FENCE = re.compile(r"^\s*(```|~~~)")
INLINE_CODE = re.compile(r"`[^`\n]*`")
# [text](target), [text](target "title"), ![alt](target); the target may be wrapped in <>.
INLINE_LINK = re.compile(r"!?\[[^\]]*\]\(\s*(?:<([^>]*)>|([^\s)]+))(?:\s+\"[^\"]*\")?\s*\)")
REF_DEF = re.compile(r"^\s{0,3}\[[^\]]+\]:\s*(?:<([^>]*)>|(\S+))")
HEADING = re.compile(r"^\s{0,3}(#{1,6})\s+(.*?)\s*#*\s*$")
HTML_ANCHOR = re.compile(r"""<a\s+(?:name|id)\s*=\s*["']([^"']+)["']""")


def markdown_files(paths: list[Path]) -> list[Path]:
    files: list[Path] = []
    for p in paths:
        if p.is_file():
            if p.suffix.lower() == ".md":
                files.append(p)
            continue
        for root, dirs, names in os.walk(p):
            dirs[:] = sorted(d for d in dirs if d not in SKIP_DIRS)
            files.extend(Path(root) / n for n in sorted(names) if n.lower().endswith(".md"))
    return sorted(set(files))


def strip_code(lines: list[str]) -> list[str]:
    """Blank out fenced code blocks and inline code so their brackets are not read as links."""
    out: list[str] = []
    in_fence = False
    for line in lines:
        if FENCE.match(line):
            in_fence = not in_fence
            out.append("")
            continue
        out.append("" if in_fence else INLINE_CODE.sub("", line))
    return out


def slugify(text: str) -> str:
    """GitHub's heading slug: lowercase, drop punctuation, spaces to hyphens."""
    text = re.sub(r"!?\[([^\]]*)\]\([^)]*\)", r"\1", text)  # links inside headings keep their text
    text = re.sub(r"[`*_~]", "", text)
    text = unicodedata.normalize("NFKC", text).strip().lower()
    text = re.sub(r"[^\w\- ]", "", text)
    return text.replace(" ", "-")


def anchors_of(md: Path, cache: dict[Path, set[str]]) -> set[str]:
    if md in cache:
        return cache[md]
    found: set[str] = set()
    seen: dict[str, int] = {}
    lines = md.read_text(encoding="utf-8", errors="replace").splitlines()
    in_fence = False
    for line in lines:
        if FENCE.match(line):
            in_fence = not in_fence
            continue
        if in_fence:
            continue
        m = HEADING.match(line)
        if m:
            slug = slugify(m.group(2))
            n = seen.get(slug, 0)
            seen[slug] = n + 1
            found.add(slug if n == 0 else f"{slug}-{n}")
        for a in HTML_ANCHOR.findall(line):
            found.add(a)
    cache[md] = found
    return found


def targets(lines: list[str]):
    for i, line in enumerate(strip_code(lines), start=1):
        for m in INLINE_LINK.finditer(line):
            yield i, (m.group(1) if m.group(1) is not None else m.group(2)).strip()
        m = REF_DEF.match(line)
        if m:
            yield i, (m.group(1) if m.group(1) is not None else m.group(2)).strip()


def check(files: list[Path], check_anchors: bool) -> list[str]:
    problems: list[str] = []
    cache: dict[Path, set[str]] = {}
    for md in files:
        lines = md.read_text(encoding="utf-8", errors="replace").splitlines()
        for lineno, target in targets(lines):
            if not target or SCHEME.match(target) or target.startswith("//"):
                continue
            path_part, _, anchor = target.partition("#")
            path_part = unquote(path_part)
            dest = md if not path_part else (md.parent / path_part).resolve()
            if not dest.exists():
                problems.append(f"{md}:{lineno}: missing target {target!r}")
                continue
            if anchor and check_anchors and dest.is_file() and dest.suffix.lower() == ".md":
                if unquote(anchor).lower() not in anchors_of(dest, cache):
                    problems.append(f"{md}:{lineno}: no heading {anchor!r} in {dest}")
    return problems


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("paths", nargs="*", help="files or directories (default: the repo root)")
    parser.add_argument("--no-anchors", action="store_true", help="do not check #anchors")
    parser.add_argument("--quiet", action="store_true", help="print problems only")
    args = parser.parse_args()

    repo = Path(__file__).resolve().parents[2]
    paths = [Path(p).resolve() for p in args.paths] or [repo]
    files = markdown_files(paths)
    problems = check(files, check_anchors=not args.no_anchors)
    for p in problems:
        try:
            p = p.replace(str(repo) + os.sep, "")
        except ValueError:
            pass
        print(p)
    if not args.quiet:
        print(f"{len(files)} Markdown files, {len(problems)} broken links")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
