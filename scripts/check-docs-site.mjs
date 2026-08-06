#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

const root = process.cwd();
const bin = process.env.GUM_BIN || path.join(root, "apps", "gum", "gum");
const docsDir = path.join(root, "docs");
const commandsDir = path.join(docsDir, "commands");
const markdownExcludes = [
  /^AGENTS\.md$/,
  /^PROCESS\.md$/,
  /^RELEASE\.md$/,
  /^known-divergences\.md$/,
  /^release-notes-template\.md$/,
  /^research\//,
  /^releases\//,
  /^commands\.generated\.md$/,
  /^spec\.md$/,
];

const requiredDocs = [
  "index.md",
  "why-gum.md",
  "install.md",
  "quickstart.md",
  "auth.md",
  "mcp.md",
  "agent-setup.md",
  "automation.md",
  "api-workflows.md",
  "safety.md",
  "output.md",
  "service-coverage.md",
  "services/README.md",
  "paths.md",
  "live-testing.md",
  "plugins.md",
  "hasp.md",
  "commands/README.md",
];

const failures = [];
for (const rel of requiredDocs) {
  if (!fs.existsSync(path.join(docsDir, rel))) failures.push(`missing docs page: docs/${rel}`);
}

if (fs.existsSync(bin)) {
  const schema = JSON.parse(execFileSync(bin, ["schema", "--json"], { encoding: "utf8", maxBuffer: 16 * 1024 * 1024 }));
  const seen = new Map();
  for (const command of walk(schema.command || {})) {
    const base = commandSlug(command);
    const count = seen.get(base) || 0;
    seen.set(base, count + 1);
    const slug = count === 0 ? base : `${base}-${count + 1}`;
    const page = path.join(commandsDir, `${slug}.md`);
    if (!fs.existsSync(page)) failures.push(`missing command doc: ${path.relative(root, page)}`);
  }
} else {
  failures.push(`missing gum binary: ${path.relative(root, bin)}; run make gum-build`);
}

const catalogPath = path.join(root, "apps", "gum", "internal", "embedded", "catalog.json");
if (fs.existsSync(catalogPath)) {
  const catalog = JSON.parse(fs.readFileSync(catalogPath, "utf8"));
  const services = new Set((catalog.ops || []).map((op) => op.service || String(op.op_id || "").split(".")[0]).filter(Boolean));
  for (const service of services) {
    const page = path.join(docsDir, "services", `${slugify(service)}.md`);
    if (!fs.existsSync(page)) failures.push(`missing service doc: ${path.relative(root, page)}`);
  }
} else {
  failures.push(`missing catalog: ${path.relative(root, catalogPath)}`);
}

for (const item of checkMarkdownLinks(docsDir)) {
  failures.push(`broken markdown link: ${item}`);
}

if (failures.length) {
  for (const failure of failures) console.error(failure);
  process.exit(1);
}

console.log(`docs check ok: ${requiredDocs.length} guide pages plus generated command docs`);

function* walk(command) {
  yield command;
  for (const child of command.subcommands || []) yield* walk(child);
}

function commandSlug(command) {
  return (command.path || command.name || "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "") || "gum";
}

function checkMarkdownLinks(dir) {
  const broken = [];
  for (const file of allMarkdown(dir)) {
    const rel = path.relative(docsDir, file).replaceAll(path.sep, "/");
    if (markdownExcludes.some((re) => re.test(rel))) continue;
    const markdown = fs.readFileSync(file, "utf8");
    const headings = headingAnchors(markdown);
    const linkPattern = /!?\[[^\]]*\]\(([^)]+)\)/g;
    let match;
    while ((match = linkPattern.exec(markdown)) !== null) {
      const target = splitMarkdownTarget(match[1].trim());
      if (!target || /^[a-z][a-z0-9+.-]*:/i.test(target)) continue;
      const [rawPath, rawAnchor] = target.split("#", 2);
      if (!rawPath && rawAnchor) {
        if (!headings.has(decode(rawAnchor))) broken.push(`${path.relative(root, file)} -> ${target}`);
        continue;
      }
      if (!rawPath) continue;
      const resolved = path.resolve(path.dirname(file), decode(rawPath));
      if (!fs.existsSync(resolved)) {
        broken.push(`${path.relative(root, file)} -> ${target}`);
        continue;
      }
      if (rawAnchor && resolved.endsWith(".md")) {
        const targetHeadings = resolved === file ? headings : headingAnchors(fs.readFileSync(resolved, "utf8"));
        if (!targetHeadings.has(decode(rawAnchor))) broken.push(`${path.relative(root, file)} -> ${target}`);
      }
    }
  }
  return broken;
}

function splitMarkdownTarget(rawTarget) {
  return rawTarget.replace(/\s+["'][^"']*["']\s*$/, "").replace(/^<|>$/g, "");
}

function decode(value) {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

function headingAnchors(markdown) {
  const anchors = new Set();
  const counts = new Map();
  let fence = false;
  for (const raw of markdown.split(/\r?\n/)) {
    if (/^```/.test(raw)) {
      fence = !fence;
      continue;
    }
    if (fence) continue;
    const match = raw.match(/^(#{1,6})\s+(.+)$/);
    if (!match) continue;
    const base = slugify(match[2].replace(/\s+#+\s*$/, ""));
    if (!base) continue;
    const count = counts.get(base) || 0;
    counts.set(base, count + 1);
    anchors.add(count ? `${base}-${count}` : base);
  }
  return anchors;
}

function slugify(text) {
  return text
    .replace(/<[^>]*>/g, "")
    .replace(/`/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9\s-]/g, "")
    .trim()
    .replace(/\s+/g, "-");
}

function allMarkdown(dir) {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) return allMarkdown(full);
    return entry.name.endsWith(".md") ? [full] : [];
  });
}
