#!/usr/bin/env python3
from __future__ import annotations

import json
import re
import subprocess
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parent.parent
MANIFEST = ROOT / "scripts" / "public-release-manifest.json"

FORBIDDEN_TEXT = (
    "Copyright 2026 Turek",
    "github.com/<owner>/gum",
    "GUM_VERSION=v0.1.0",
    "docs/release-notes-v0.1.0.md",
    "SLSA L3",
    "Homebrew tap PR merged",
    "github.com/ehmo/gounmc",
    "ehmo/gounmc",
    "gum-owned OAuth client",
    "private repo",
    "private source",
    ".beads",
    "AGENTS.md",
    "CLAUDE.md",
)

DOC_FORBIDDEN_TEXT = (
    "docs/RELEASE.md",
    "docs/spec.md",
    "docs/known-divergences.md",
    "docs/research",
    "docs/auth-managed-scopes.v1.json",
    "managed OAuth",
    "Beads",
    "bd ",
)

LOCAL_PATH_PATTERNS = (
    re.compile(r"(?<![A-Za-z0-9])/(?:Users|Volumes)/[^\s\"'`<>)\]]+"),
    re.compile(r"(?<![A-Za-z0-9])/(?:home|root)/[^\s\"'`<>)\]]+"),
    re.compile(r"(?i)\b[A-Z]:\\Users\\[^\s\"'`<>)\]]+"),
)

BINARY_SUFFIXES = {
    ".gif",
    ".jpeg",
    ".jpg",
    ".png",
    ".webp",
}

FENCE_RE = re.compile(r"^```(?P<lang>[A-Za-z0-9_+.#-]*)\s*$")

# Files whose recipes run helper scripts. Every scripts/ path they name has to
# be exported, or the public checkout has a Makefile target and a CI job that
# cannot run. `make docs-check` shipped broken this way once: the root Makefile
# called scripts/gen-service-reference.mjs and the manifest never listed it.
SCRIPT_CALLERS = (
    "Makefile",
    ".github/workflows/test.yml",
    ".github/workflows/release.yml",
)
SCRIPT_REF_RE = re.compile(r"scripts/[A-Za-z0-9_.-]+\.(?:mjs|js|py|sh)")

# Same failure shape for images: the docs-site build fails the whole job on a
# broken link, so every `assets/<file>` named by an exported page or by the
# site generator has to be exported as docs/assets/<file>.
ASSET_REF_RE = re.compile(r"assets/([A-Za-z0-9_.-]+\.(?:png|jpg|jpeg|webp|gif|svg|ico))")

# And for page-to-page links: docs/service-matrix.md linked
# docs/admin-write-policy-gate.md, which the manifest never listed, so the
# public docs-site build died on the dangling href.
MD_LINK_RE = re.compile(r"\]\(([^)#?\s]+\.md)(?:[#?][^)\s]*)?\)")

# scripts/check-docs-site.mjs hard-fails on any page in its requiredDocs list
# that is not on disk, and the public docs CI job runs it. Read the list from
# the script itself so the two cannot drift.
REQUIRED_DOCS_RE = re.compile(r"const requiredDocs = \[(.*?)\];", re.S)


def fail(message: str) -> None:
    print(f"public release contract: {message}", file=sys.stderr)
    raise SystemExit(1)


def read_manifest() -> dict:
    if not MANIFEST.exists():
        fail("scripts/public-release-manifest.json is missing")
    return json.loads(MANIFEST.read_text(encoding="utf-8"))


def tracked_files() -> list[str]:
    proc = subprocess.run(
        ["git", "-C", str(ROOT), "ls-files"],
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    return [line for line in proc.stdout.splitlines() if line]


def generated_files(manifest: dict) -> list[str]:
    paths: list[str] = []
    for root in manifest.get("generated_roots", []):
        base = ROOT / root
        if not base.is_dir():
            continue
        for path in base.rglob("*"):
            if path.is_file():
                paths.append(path.relative_to(ROOT).as_posix())
    return paths


def public_files(manifest: dict) -> list[Path]:
    rels = set(manifest.get("root_files", []) + manifest.get("docs", []))
    roots = tuple(root.rstrip("/") + "/" for root in manifest.get("source_roots", []))
    excludes = set(manifest.get("exclude_paths", []))
    exclude_roots = tuple(rel.rstrip("/") + "/" for rel in excludes)
    for rel in tracked_files():
        if rel.startswith(roots):
            rels.add(rel)
    rels.update(generated_files(manifest))
    rels = {
        rel
        for rel in rels
        if rel not in excludes and not rel.startswith(exclude_roots)
    }
    if not rels:
        fail("manifest has no public files")
    files: list[Path] = []
    for rel in sorted(rels):
        path = ROOT / rel
        if not path.is_file():
            fail(f"required public file is missing: {rel}")
        files.append(path)
    return files


def validate_text(path: Path) -> None:
    rel = path.relative_to(ROOT).as_posix()
    if rel in {"scripts/check-public-release-contract.py", "scripts/public-release-manifest.json"}:
        return
    if path.suffix.lower() in BINARY_SUFFIXES:
        return
    text = path.read_text(encoding="utf-8")
    for needle in FORBIDDEN_TEXT:
        if needle in text:
            fail(f"forbidden text {needle!r} in {rel}")
    is_public_prose = rel in {"README.md", "CHANGELOG.md", "SECURITY.md"} or rel.startswith("docs/")
    if is_public_prose:
        for needle in DOC_FORBIDDEN_TEXT:
            if needle in text:
                fail(f"forbidden public-doc text {needle!r} in {rel}")
        for pattern in LOCAL_PATH_PATTERNS:
            match = pattern.search(text)
            if match:
                fail(f"local filesystem path {match.group(0)!r} in {rel}")
        validate_markdown_fences(rel, text)


def validate_markdown_fences(rel: str, text: str) -> None:
    if not rel.endswith(".md"):
        return
    in_fence = False
    opener = 0
    for lineno, line in enumerate(text.splitlines(), 1):
        match = FENCE_RE.match(line)
        if not match:
            continue
        if not in_fence:
            lang = match.group("lang")
            if not lang:
                fail(f"unlabeled markdown code fence in {rel}:{lineno}")
            in_fence = True
            opener = lineno
        else:
            in_fence = False
    if in_fence:
        fail(f"unclosed markdown code fence in {rel}:{opener}")


def main() -> int:
    manifest = read_manifest()
    required_source = {
        ".github/workflows/test.yml",
        ".github/workflows/release.yml",
        "apps/gum/go.mod",
        "apps/gum/go.sum",
        "apps/gum/cmd/gum/main.go",
        "apps/gum/internal/mcp/server.go",
        "apps/gum/internal/embedded/catalog.json",
        "scripts/check-public-release-contract.py",
    }
    selected = {path.relative_to(ROOT).as_posix() for path in public_files(manifest)}
    if (ROOT / ".gitignore").is_file() and ".beads" not in (ROOT / ".gitignore").read_text(encoding="utf-8"):
        selected.add(".gitignore")
    missing_source = sorted(required_source - selected)
    if missing_source:
        fail(f"public export is not buildable; missing {missing_source[0]}")
    for rel in manifest.get("forbidden_public_paths", []):
        if (ROOT / rel).exists() and rel in selected:
            fail(f"forbidden path is listed for public export: {rel}")
        forbidden_prefix = rel.rstrip("/") + "/"
        leaked = sorted(path for path in selected if path.startswith(forbidden_prefix))
        if leaked:
            fail(f"forbidden path is selected for public export: {leaked[0]}")
    for caller in SCRIPT_CALLERS:
        source = ROOT / caller
        if not source.is_file():
            continue
        for ref in sorted(set(SCRIPT_REF_RE.findall(source.read_text(encoding="utf-8")))):
            if ref not in selected:
                fail(f"{caller} runs {ref}, which the public export does not ship")

    for rel in sorted(selected):
        if not (rel.endswith(".md") or rel.endswith(".mjs")):
            continue
        for asset in sorted(set(ASSET_REF_RE.findall((ROOT / rel).read_text(encoding="utf-8")))):
            if f"docs/assets/{asset}" not in selected:
                fail(f"{rel} references assets/{asset}, which the public export does not ship")

    for rel in sorted(selected):
        if not rel.startswith("docs/") or not rel.endswith(".md"):
            continue
        base = (ROOT / rel).parent
        for link in sorted(set(MD_LINK_RE.findall((ROOT / rel).read_text(encoding="utf-8")))):
            if link.startswith(("http://", "https://")):
                continue
            target = (base / link).resolve()
            try:
                target_rel = target.relative_to(ROOT).as_posix()
            except ValueError:
                fail(f"{rel} links outside the repository: {link}")
            if target_rel not in selected:
                fail(f"{rel} links to {target_rel}, which the public export does not ship")

    checker = ROOT / "scripts" / "check-docs-site.mjs"
    if checker.is_file():
        block = REQUIRED_DOCS_RE.search(checker.read_text(encoding="utf-8"))
        if not block:
            fail("cannot read requiredDocs from scripts/check-docs-site.mjs")
        required_docs = re.findall(r'"([^"]+)"', block.group(1))
        if not required_docs:
            fail("requiredDocs in scripts/check-docs-site.mjs is empty")
        for rel in required_docs:
            if f"docs/{rel}" not in selected:
                fail(f"check-docs-site.mjs requires docs/{rel}, which the public export does not ship")

    for path in sorted((ROOT / rel for rel in selected), key=lambda p: p.as_posix()):
        validate_text(path)

    license_text = (ROOT / "LICENSE").read_text(encoding="utf-8")
    required_license_fragments = [
        "MIT License",
        "Copyright (c) 2026 Wraxle LLC",
        "Permission is hereby granted, free of charge",
    ]
    for fragment in required_license_fragments:
        if fragment not in license_text:
            fail(f"LICENSE must use MIT terms; missing {fragment!r}")

    goreleaser_config = (ROOT / "apps" / "gum" / ".goreleaser.yaml").read_text(encoding="utf-8")
    if re.search(r"(?m)^source:\n\s+enabled:\s+true\b", goreleaser_config):
        fail("GoReleaser source archive must stay disabled until public export scanning exists")

    release_workflow = (ROOT / ".github" / "workflows" / "release.yml").read_text(encoding="utf-8")
    if "github.repository == 'ehmo/gum'" not in release_workflow:
        fail("release workflow must be guarded to publish only from ehmo/gum")

    print("public release contract: ok")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
