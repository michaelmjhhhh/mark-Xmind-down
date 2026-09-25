#!/usr/bin/env python3
"""Independent sample oracle using Pandoc's GFM parser (no project Go parsers).

Run from the checkout: python3 scripts/qa_samples.py
Requires Go and Pandoc. Regenerates exports/ and writes exports/qa/report.json.
The sample oracle deliberately rejects unmodeled content instead of ignoring it.
"""

import hashlib
import json
from pathlib import Path
import subprocess
from urllib.parse import unquote, urlsplit
import zipfile


ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "exports"


def normalized(text):
    return " ".join(text.split())


def inlines(items):
    text, images = [], []
    for item in items:
        kind, value = item["t"], item.get("c")
        if kind == "Str":
            text.append(value)
        elif kind in ("Space", "SoftBreak", "LineBreak"):
            text.append(" ")
        elif kind == "Image":
            images.append(value[2][0])
        elif kind == "Link":
            label, nested = inlines(value[1])
            text.append(label)
            images.extend(nested)
        elif kind == "RawInline" and value == ["html", "<br>"]:
            text.append(" ")
        else:
            raise AssertionError(f"Unexpected inline formatting in sample: {item}")
    return normalized("".join(text)), images


def parsed_tree(blocks):
    roots, current_root, current = [], None, None
    sheet_title = None

    def item_node(blocks):
        assert blocks and blocks[0]["t"] in ("Plain", "Para"), blocks
        title, images = inlines(blocks[0]["c"])
        node = {"title": title, "images": images, "children": []}
        for block in blocks[1:]:
            if block["t"] == "BulletList":
                node["children"].extend(item_node(x) for x in block["c"])
            elif block["t"] in ("Para", "Plain"):
                text, more_images = inlines(block["c"])
                assert not text and more_images, block
                node["images"].extend(more_images)
            else:
                raise AssertionError(f"Unexpected sample block: {block['t']}")
        return node

    for block in blocks:
        kind, value = block["t"], block.get("c")
        if kind == "Header":
            title, images = inlines(value[2])
            current = {"title": title, "images": images, "children": []}
            if value[0] == 1:
                current_root = current
                current_root["sheet"] = sheet_title
                roots.append(current_root)
                sheet_title = None
            else:
                assert value[0] == 2 and current_root is not None
                current_root["children"].append(current)
        elif kind == "BulletList":
            assert current is not None
            current["children"].extend(item_node(x) for x in value)
        elif kind == "Para":
            text, images = inlines(value)
            if text.startswith("Sheet: "):
                sheet_title = text.removeprefix("Sheet: ")
            else:
                assert current is not None and not text and images, block
                current["images"].extend(images)
        elif kind == "HorizontalRule":
            current = current_root = None
        else:
            raise AssertionError(f"Unexpected sample block: {kind}")
    return roots


def verify_sample(source):
    md = OUT / (source.stem + ".md")
    ast = json.loads(subprocess.check_output(["pandoc", "-f", "gfm", "-t", "json", str(md)]))
    actual = parsed_tree(ast["blocks"])
    counts = {"topics": 0, "images": 0, "image_bytes": 0, "max_depth": 0}
    seen = set()
    with zipfile.ZipFile(source) as archive:
        sheets = json.loads(archive.read("content.json"))
        assert len(sheets) == len(actual)

        def compare(topic, node, depth=0):
            # Additional sample content must get an explicit oracle, not a silent skip.
            assert not any(topic.get(key) for key in ("notes", "href", "labels", "markers", "boundaries", "summaries", "extensions"))
            groups = topic.get("children", {})
            assert set(groups) <= {"attached"}
            title = topic.get("title", "")
            if not title and not topic.get("image"):
                title = "Untitled topic"
            assert normalized(title) == node["title"], (title, node["title"])
            counts["topics"] += 1
            counts["max_depth"] = max(counts["max_depth"], depth)
            expected_images = [topic["image"]["src"]] if topic.get("image") else []
            assert len(expected_images) == len(node["images"]), title
            for ref, link in zip(expected_images, node["images"]):
                original = archive.read(unquote(ref.removeprefix("xap:").lstrip("/")))
                url = urlsplit(link)
                assert not url.scheme and not url.netloc and not url.query and not url.fragment
                assert url.path.startswith("assets/") and ".." not in url.path.split("/")
                destination = OUT / unquote(url.path)
                assert destination.read_bytes() == original, link
                digest = hashlib.sha256(original).hexdigest()
                assert destination.stem == digest
                counts["images"] += 1
                counts["image_bytes"] += len(original)
                seen.add(destination.name)
            children = groups.get("attached", [])
            assert len(children) == len(node["children"]), title
            for child, rendered in zip(children, node["children"]):
                compare(child, rendered, depth + 1)

        for sheet, tree in zip(sheets, actual):
            name = sheet.get("title", "")
            expected_name = normalized(name) if name and name != sheet["rootTopic"].get("title", "") else None
            assert tree["sheet"] == expected_name
            compare(sheet["rootTopic"], tree)

    subprocess.run(["pandoc", "-f", "gfm", "-t", "html5", "--standalone", "--metadata", "pagetitle=" + source.stem, "--css", "qa.css", str(md), "-o", str(md.with_suffix(".html"))], check=True)
    return {"sample": source.name, "sheets": len(actual), **counts, "unique_images": len(seen), "status": "PASS"}, seen


def main():
    subprocess.run(["go", "run", "./cmd/xmind-md", "assets", "--output-dir", "exports", "--force"], cwd=ROOT, check=True)
    (OUT / "qa").mkdir(exist_ok=True)
    (OUT / "qa.css").write_text("body{max-width:960px;margin:32px auto;padding:0 24px;font:16px/1.6 system-ui,sans-serif;color:#20252d;background:white}img{max-width:100%;height:auto}h1{font-size:30px;line-height:1.25}h2{margin-top:36px;font-size:22px}li{margin:6px 0}blockquote{border-left:3px solid #ccc;margin-left:0;padding-left:18px}")
    results, seen = [], set()
    for source in sorted((ROOT / "assets").glob("*.xmind")):
        result, names = verify_sample(source)
        results.append(result)
        seen.update(names)
        print(json.dumps(result, ensure_ascii=False))
    report = {"oracle": subprocess.check_output(["pandoc", "--version"], text=True).splitlines()[0], "samples": results, "referenced_assets": len(seen), "checks": ["rendered topic text, order, and parent hierarchy", "sheet titles", "no unintended Markdown blocks", "per-occurrence image association", "portable relative URLs", "original image bytes and SHA-256 filenames"]}
    (OUT / "qa" / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")


if __name__ == "__main__":
    main()
