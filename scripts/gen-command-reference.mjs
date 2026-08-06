#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

const root = process.cwd();
const bin = process.env.GUM_BIN || path.join(root, "apps", "gum", "gum");
const docsDir = path.join(root, "docs");
const commandsDir = path.join(docsDir, "commands");
const generatedIndex = path.join(docsDir, "commands.generated.md");

if (!fs.existsSync(bin)) {
  execFileSync("make", ["-C", path.join(root, "apps", "gum"), "build"], { stdio: "inherit" });
}

const schema = JSON.parse(execFileSync(bin, ["schema", "--json"], {
  encoding: "utf8",
  maxBuffer: 16 * 1024 * 1024,
}));

const commands = Array.from(walk(schema.command || {}));
const slugCounts = new Map();
for (const command of commands) {
  const base = commandSlug(command);
  const seen = slugCounts.get(base) || 0;
  slugCounts.set(base, seen + 1);
  command._slug = seen === 0 ? base : `${base}-${seen + 1}`;
}

fs.rmSync(commandsDir, { recursive: true, force: true });
fs.mkdirSync(commandsDir, { recursive: true });
fs.writeFileSync(path.join(commandsDir, "README.md"), commandIndex(), "utf8");
fs.writeFileSync(generatedIndex, commandReference(), "utf8");
for (const command of commands) {
  fs.writeFileSync(path.join(commandsDir, `${command._slug}.md`), commandPage(command), "utf8");
}

console.log(`generated ${commands.length} command pages in ${path.relative(root, commandsDir)}`);

function* walk(command, parent = null, depth = 0) {
  command._parent = parent;
  command._depth = depth;
  yield command;
  for (const child of command.subcommands || []) {
    yield* walk(child, command, depth + 1);
  }
}

function canonicalPath(command) {
  return (command.path || command.name || "")
    .split(/\s+/)
    .filter((part) => part && !(part.startsWith("(") && part.endsWith(")")))
    .join(" ");
}

function commandSlug(command) {
  return canonicalPath(command)
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "") || "gum";
}

function commandLabel(command) {
  const pathName = canonicalPath(command);
  const usage = (command.usage || "").trim();
  return usage ? `${pathName} ${usage}` : pathName;
}

function firstLine(value) {
  return (value || "").trim().split(/\r?\n/)[0] || "";
}

function mdEscape(value) {
  return String(value || "").replace(/\|/g, "\\|").replace(/\r?\n/g, "<br>");
}

function link(command, label = canonicalPath(command)) {
  return `[${label}](${command._slug}.md)`;
}

function commandReference() {
  const lines = [
    "# Command Reference",
    "",
    "Generated from `gum schema --json`.",
    "",
  ];
  for (const command of commands) {
    const label = commandLabel(command);
    const summary = firstLine(command.help);
    const indent = "  ".repeat(Math.max(command._depth, 0));
    const target = `commands/${command._slug}.md`;
    lines.push(summary ? `${indent}- [\`${label}\`](${target}) - ${summary}` : `${indent}- [\`${label}\`](${target})`);
  }
  lines.push("");
  return lines.join("\n");
}

function commandIndex() {
  const top = commands.filter((command) => command._depth === 1);
  const lines = [
    "# Commands",
    "",
    "Every `gum` CLI command has a generated docs page. Product coverage is documented under [Operations by service](../services/); this index is for command names, flags, aliases, arguments, and generated help.",
    "",
    `Generated pages: ${commands.length}.`,
    "",
    "## Top-level commands",
    "",
  ];
  for (const command of top) {
    const summary = firstLine(command.help);
    lines.push(summary ? `- ${link(command)} - ${summary}` : `- ${link(command)}`);
  }
  lines.push("", "## All commands", "");
  for (const command of commands) {
    const summary = firstLine(command.help);
    const indent = "  ".repeat(Math.max(command._depth, 0));
    lines.push(summary ? `${indent}- ${link(command)} - ${summary}` : `${indent}- ${link(command)}`);
  }
  lines.push("");
  return lines.join("\n");
}

function commandPage(command) {
  const parent = command._parent;
  const children = command.subcommands || [];
  const args = command.arguments || [];
  const flags = command.flags || [];
  const title = canonicalPath(command);
  const lines = [
    `# \`${title}\``,
    "",
    "> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.",
    "",
  ];
  if (command.help) {
    lines.push(command.help.trim(), "");
  }
  lines.push("## Usage", "", "```bash", commandLabel(command), "```", "");
  if (parent) {
    lines.push("## Parent", "", `- ${link(parent)}`, "");
  }
  if (children.length) {
    lines.push("## Subcommands", "");
    for (const child of children) {
      const summary = firstLine(child.help);
      lines.push(summary ? `- ${link(child)} - ${summary}` : `- ${link(child)}`);
    }
    lines.push("");
  }
  if (args.length) {
    lines.push("## Arguments", "", "| Name | Help |", "| --- | --- |");
    for (const arg of args) {
      lines.push(`| \`${mdEscape(arg.name)}\` | ${mdEscape(arg.help)} |`);
    }
    lines.push("");
  }
  if (flags.length) {
    lines.push("## Flags", "", "| Flag | Type | Default | Help |", "| --- | --- | --- | --- |");
    for (const flag of flags) {
      const names = [];
      if (flag.short) names.push(`\`-${flag.short}\``);
      names.push(`\`--${flag.name}\``);
      for (const alias of flag.aliases || []) names.push(`\`--${alias}\``);
      lines.push(`| ${names.join("<br>")} | \`${mdEscape(flag.type)}\` | ${mdEscape(flag.has_default ? flag.default : "")} | ${mdEscape(flag.help)} |`);
    }
    lines.push("");
  }
  lines.push("## See also", "");
  if (parent) lines.push(`- ${link(parent)}`);
  lines.push("- [Command index](README.md)", "");
  return lines.join("\n");
}
