#!/usr/bin/env python3
"""Render mischief's Markdown docs to their HTML twins.

Why this exists: the .md files are the design authority, but the .html twins are
what get read in a browser. They were produced by hand, so any edit to a .md
silently left the HTML asserting the old design — the same prose-drift failure
crier closed with `make docs-check`. Generating one from the other removes the
whole class: the HTML is now a build output, not a second source of truth.

Deliberately small: headings, paragraphs, lists, blockquotes, fenced code, GFM
tables, and inline code/bold/italic/links. If a doc needs more than that, extend
here rather than hand-editing the HTML.

Usage: python3 tools/gen-docs.py [--check]
       --check exits 1 if any HTML is stale (for CI), without writing.
"""
import html
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent

# Same palette/token shape the hand-made twins used, so the output is visually
# continuous with the docs already in the tree.
CSS = """
:root{--bg:#0f1115;--fg:#d7dae0;--mut:#8b93a1;--acc:#5eb0ef;--card:#161a22;--bd:#242a35}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--fg);
font:17px/1.65 -apple-system,'Segoe UI',Roboto,sans-serif;padding:1.1rem;max-width:52rem;margin-inline:auto}
h1{font-size:1.5rem;line-height:1.3;color:#fff;margin:1.2rem 0 .6rem}
h2{font-size:1.22rem;color:#fff;margin:2.2rem 0 .6rem;border-bottom:1px solid var(--bd);padding-bottom:.3rem}
h3{font-size:1.02rem;color:var(--acc);margin:1.5rem 0 .4rem}
h4{font-size:.95rem;color:#fff;margin:1.2rem 0 .3rem}
a{color:var(--acc)}
code{background:#1b2028;padding:.12em .35em;border-radius:4px;font-size:.88em}
pre{background:var(--card);border:1px solid var(--bd);border-radius:6px;padding:.9rem;overflow-x:auto;font-size:.85rem}
pre code{background:none;padding:0}
blockquote{border-left:3px solid var(--acc);margin:1rem 0;padding:.2rem 1rem;color:var(--mut)}
table{border-collapse:collapse;width:100%;font-size:.9rem;margin:1rem 0;display:block;overflow-x:auto}
th,td{border:1px solid var(--bd);padding:.45rem .6rem;text-align:left}
th{background:var(--card);color:#fff}
ul,ol{padding-left:1.4rem}
li{margin:.2rem 0}
hr{border:0;border-top:1px solid var(--bd);margin:1.6rem 0}
@media(max-width:600px){body{font-size:15px;padding:.8rem}}
""".strip()


def inline(s: str) -> str:
    """Escape, then apply the inline spans, code first so its content is inert."""
    s = html.escape(s, quote=False)
    codes = []

    def stash(m):
        codes.append(m.group(1))
        return f"\x00{len(codes) - 1}\x00"

    s = re.sub(r"`([^`]+)`", stash, s)
    s = re.sub(r"\*\*([^*]+)\*\*", r"<strong>\1</strong>", s)
    s = re.sub(r"(?<!\*)\*([^*]+)\*(?!\*)", r"<em>\1</em>", s)
    s = re.sub(r"\[([^\]]+)\]\(([^)]+)\)", r'<a href="\2">\1</a>', s)
    for i, c in enumerate(codes):
        s = s.replace(f"\x00{i}\x00", f"<code>{c}</code>")
    return s


def render(md: str) -> str:
    out, i = [], 0
    lines = md.split("\n")

    while i < len(lines):
        line = lines[i]

        # fenced code
        if line.startswith("```"):
            i += 1
            buf = []
            while i < len(lines) and not lines[i].startswith("```"):
                buf.append(lines[i])
                i += 1
            i += 1
            out.append("<pre><code>" + html.escape("\n".join(buf)) + "</code></pre>")
            continue

        # GFM table: a header row followed by a separator row
        if "|" in line and i + 1 < len(lines) and re.match(r"^\s*\|?[\s:|-]+\|[\s:|-]*$", lines[i + 1]):
            def cells(row):
                return [c.strip() for c in row.strip().strip("|").split("|")]
            head = cells(line)
            i += 2
            body = []
            while i < len(lines) and "|" in lines[i] and lines[i].strip():
                body.append(cells(lines[i]))
                i += 1
            out.append("<table><thead><tr>" + "".join(f"<th>{inline(h)}</th>" for h in head) + "</tr></thead><tbody>")
            for r in body:
                out.append("<tr>" + "".join(f"<td>{inline(c)}</td>" for c in r) + "</tr>")
            out.append("</tbody></table>")
            continue

        m = re.match(r"^(#{1,6})\s+(.*)$", line)
        if m:
            lvl = len(m.group(1))
            out.append(f"<h{lvl}>{inline(m.group(2))}</h{lvl}>")
            i += 1
            continue

        if re.match(r"^\s*([-*_])\s*\1\s*\1", line):
            out.append("<hr>")
            i += 1
            continue

        if line.startswith(">"):
            buf = []
            while i < len(lines) and lines[i].startswith(">"):
                buf.append(lines[i].lstrip(">").strip())
                i += 1
            out.append("<blockquote><p>" + inline(" ".join(buf)) + "</p></blockquote>")
            continue

        m = re.match(r"^\s*[-*]\s+(.*)$", line)
        if m:
            items = []
            while i < len(lines) and re.match(r"^\s*[-*]\s+", lines[i]):
                items.append(re.match(r"^\s*[-*]\s+(.*)$", lines[i]).group(1))
                i += 1
            out.append("<ul>" + "".join(f"<li>{inline(x)}</li>" for x in items) + "</ul>")
            continue

        m = re.match(r"^\s*\d+\.\s+(.*)$", line)
        if m:
            items = []
            while i < len(lines) and re.match(r"^\s*\d+\.\s+", lines[i]):
                items.append(re.match(r"^\s*\d+\.\s+(.*)$", lines[i]).group(1))
                i += 1
            out.append("<ol>" + "".join(f"<li>{inline(x)}</li>" for x in items) + "</ol>")
            continue

        if not line.strip():
            i += 1
            continue

        buf = []
        while i < len(lines) and lines[i].strip() and not re.match(r"^(#{1,6}\s|```|>|\s*[-*]\s|\s*\d+\.\s)", lines[i]) and "|" not in lines[i]:
            buf.append(lines[i].strip())
            i += 1
        if buf:
            out.append("<p>" + inline(" ".join(buf)) + "</p>")
        else:
            i += 1

    return "\n".join(out)


def main() -> int:
    check = "--check" in sys.argv
    stale = []
    for md in sorted(ROOT.rglob("*.md")):
        if ".git" in md.parts:
            continue
        target = md.with_suffix(".html")
        if not target.exists():
            continue  # only refresh twins that already exist; don't create new ones
        body = render(md.read_text(encoding="utf-8"))
        title = md.stem.replace("-", " ").title()
        doc = (
            '<!DOCTYPE html><html lang="en"><head><meta charset="utf-8">\n'
            '<meta name="viewport" content="width=device-width, initial-scale=1">\n'
            f"<title>{html.escape(title)}</title>\n<style>\n{CSS}\n</style></head>\n<body>\n"
            f"{body}\n</body></html>\n"
        )
        if target.read_text(encoding="utf-8") != doc:
            stale.append(target)
            if not check:
                target.write_text(doc, encoding="utf-8")

    if check:
        for t in stale:
            print(f"STALE: {t.relative_to(ROOT)}")
        print(f"{'FAIL' if stale else 'OK'} — {len(stale)} stale twin(s)")
        return 1 if stale else 0

    for t in stale:
        print(f"rendered: {t.relative_to(ROOT)}")
    print(f"{len(stale)} file(s) written")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
