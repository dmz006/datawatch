#!/usr/bin/env python3
"""
Append a "View fullscreen in Mermaid Live Editor" link after every
```mermaid block in every .md file under docs/. Links are generated
in the mermaid.live/view#pako:<base64> format that the editor accepts
directly, so clicking the link opens a zoomable + pan-able render of
that exact diagram.

Idempotent — a line matching `<sub>.*mermaid.live/view.*</sub>`
immediately after a block is replaced, not duplicated.

Usage:
  python3 scripts/mermaid-fullscreen-links.py         # rewrite in place
  python3 scripts/mermaid-fullscreen-links.py --dry   # preview
"""
import sys, re, os, json, base64, zlib, pathlib


DOCS_ROOT = pathlib.Path(__file__).resolve().parent.parent / "docs"
LINK_RE = re.compile(r"^<sub>.*mermaid\.live/view.*</sub>\s*$", re.MULTILINE)


def pako_encode(code: str) -> str:
    """
    Produce the `pako:<base64>` payload that mermaid.live understands.
    It's a zlib-deflated JSON envelope, base64-urlsafe encoded without
    padding. The editor's decoder mirrors pako.js.
    """
    envelope = {
        "code": code,
        "mermaid": "{}",
        "updateEditor": False,
        "autoSync": True,
        "updateDiagram": False,
    }
    raw = json.dumps(envelope, separators=(",", ":")).encode("utf-8")
    compressed = zlib.compress(raw, level=9)
    b64 = base64.urlsafe_b64encode(compressed).decode("ascii").rstrip("=")
    return "pako:" + b64


def transform(text: str) -> tuple[str, int]:
    """Return (new_text, blocks_modified)."""
    # Match ```mermaid\n...``` (non-greedy).
    pat = re.compile(r"```mermaid\s*\n(.*?)```", re.DOTALL)
    count = 0

    def repl(m: re.Match) -> str:
        nonlocal count
        count += 1
        src = m.group(1).rstrip("\n")
        url = "https://mermaid.live/view#" + pako_encode(src)
        block = m.group(0)
        link_line = f"<sub>🔍 <a href=\"{url}\">View this diagram fullscreen (zoom &amp; pan)</a></sub>"
        return f"{block}\n{link_line}"

    # First strip any stale links inserted by a previous run so we
    # don't stack them.
    text = LINK_RE.sub("", text)
    # Collapse excess blank lines the strip may have left.
    text = re.sub(r"\n{3,}", "\n\n", text)
    new = pat.sub(repl, text)
    return new, count


def main() -> int:
    dry = "--dry" in sys.argv
    total_files = total_blocks = 0
    for md in DOCS_ROOT.rglob("*.md"):
        orig = md.read_text(encoding="utf-8")
        if "```mermaid" not in orig:
            continue
        new, n = transform(orig)
        if new != orig:
            total_files += 1
            total_blocks += n
            if dry:
                print(f"[dry] would update {md.relative_to(DOCS_ROOT.parent)} — {n} block(s)")
            else:
                md.write_text(new, encoding="utf-8")
                print(f"updated {md.relative_to(DOCS_ROOT.parent)} — {n} block(s)")
    print(f"\n{total_files} file(s), {total_blocks} mermaid block(s) " + ("previewed" if dry else "annotated"))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
