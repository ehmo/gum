#!/usr/bin/env node
import fs from "node:fs";
import path from "node:path";

const root = process.cwd();
const docsDir = path.join(root, "docs");
const outDir = path.join(root, "dist", "docs-site");
const repoBase = "https://github.com/ehmo/gum";
const siteOrigin = "https://gumcli.dev";
const socialImage = `${siteOrigin}/assets/gum-social-card-20260624.png`;
const productName = "gum";
const productTagline = "Google APIs for agents and terminals";
const productDescription = "A single Go CLI and MCP stdio server for safer Google API access from agents, scripts, and humans.";
const installCommand = "brew install ehmo/tap/gum";

const baseSections = [
  ["Start", ["index.md", "why-gum.md", "install.md", "quickstart.md", "auth.md", "mcp.md"]],
  ["Agent Workflows", ["agent-setup.md", "automation.md", "api-workflows.md", "safety.md", "output.md", "hasp.md"]],
  ["Google APIs", ["service-coverage.md", "services/README.md", "auth-guides/README.md", "field-masks.md", "paths.md", "live-testing.md"]],
  ["Plugins", ["plugins.md", "plugin-contract.md", "plugin-author-guide.md"]],
  ["Reference", ["commands/README.md", "architecture.md", "catalog-abi.md", "expression-profile-dsl.md", "test-matrix.md"]],
  ["Project", ["changelog.md", "license.md", "security.md", "support.md"]],
];

const serviceGroupOrder = [
  "Workspace documents",
  "Workspace communication",
  "Workspace administration",
  "People and education",
  "Search and media",
  "Ads and maps",
  "Research and travel",
  "Internal",
];
const serviceGroupsBySlug = new Map([
  ["docs", "Workspace documents"],
  ["drive", "Workspace documents"],
  ["forms", "Workspace documents"],
  ["script", "Workspace documents"],
  ["sheets", "Workspace documents"],
  ["slides", "Workspace documents"],
  ["calendar", "Workspace communication"],
  ["chat", "Workspace communication"],
  ["gmail", "Workspace communication"],
  ["meet", "Workspace communication"],
  ["tasks", "Workspace communication"],
  ["admin", "Workspace administration"],
  ["adminreports", "Workspace administration"],
  ["cloudidentity", "Workspace administration"],
  ["groupssettings", "Workspace administration"],
  ["vault", "Workspace administration"],
  ["classroom", "People and education"],
  ["people", "People and education"],
  ["customsearch", "Search and media"],
  ["indexing", "Search and media"],
  ["photoslibrary", "Search and media"],
  ["searchconsole", "Search and media"],
  ["youtube", "Search and media"],
  ["googleads", "Ads and maps"],
  ["maps", "Ads and maps"],
  ["places", "Ads and maps"],
  ["routes", "Ads and maps"],
  ["flights", "Research and travel"],
  ["patents", "Research and travel"],
  ["scholar", "Research and travel"],
  ["trends", "Research and travel"],
  ["meta", "Internal"],
]);

const buildExcludes = [
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

fs.rmSync(outDir, { recursive: true, force: true });
fs.mkdirSync(outDir, { recursive: true });

const allPages = allMarkdown(docsDir).map((file) => {
  const rel = path.relative(docsDir, file).replaceAll(path.sep, "/");
  const raw = fs.readFileSync(file, "utf8");
  const { frontmatter, body } = parseFrontmatter(raw);
  const title = frontmatter.title || firstHeading(body) || titleize(path.basename(rel, ".md"));
  return { file, rel, title, outRel: outPath(rel, frontmatter), markdown: body, frontmatter };
});
const pages = allPages.filter((page) => !buildExcludes.some((re) => re.test(page.rel)));
const pageMap = new Map(pages.map((page) => [page.rel, page]));
const servicePages = pages.filter((page) => page.rel.startsWith("services/") && page.rel !== "services/README.md")
  .sort((a, b) => a.title.localeCompare(b.title));
const sections = [
  ...baseSections.slice(0, 3),
  serviceNavSection(servicePages),
  ...baseSections.slice(3),
];
const nav = sections
  .map(normalizeSection)
  .filter((section) => section.pages.length);
const sectionByRel = new Map();
for (const section of nav) for (const page of section.pages) sectionByRel.set(page.rel, section.name);
const orderedPages = nav.flatMap((section) => section.pages);

for (const page of pages) {
  const html = markdownToHtml(page.markdown, page.rel);
  const toc = tocFromHtml(html);
  const idx = orderedPages.findIndex((candidate) => candidate.rel === page.rel);
  const prev = idx > 0 ? orderedPages[idx - 1] : null;
  const next = idx >= 0 && idx < orderedPages.length - 1 ? orderedPages[idx + 1] : null;
  const pageOut = path.join(outDir, page.outRel);
  fs.mkdirSync(path.dirname(pageOut), { recursive: true });
  fs.writeFileSync(pageOut, layout({ page, html, toc, prev, next, sectionName: sectionByRel.get(page.rel) || "Reference" }), "utf8");
}

copyStaticDir(path.join(docsDir, "assets"), path.join(outDir, "assets"));
fs.writeFileSync(path.join(outDir, ".nojekyll"), "", "utf8");
fs.writeFileSync(path.join(outDir, "llms.txt"), llmsTxt(), "utf8");
validateLinks(outDir);
console.log(`built docs site: ${path.relative(root, outDir)}`);

function parseFrontmatter(raw) {
  const match = raw.match(/^---\n([\s\S]*?)\n---\n?/);
  if (!match) return { frontmatter: {}, body: raw };
  const frontmatter = {};
  for (const line of match[1].split("\n")) {
    const m = line.match(/^([A-Za-z0-9_-]+):\s*(.*?)\s*$/);
    if (!m) continue;
    let value = m[2];
    if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'"))) {
      value = value.slice(1, -1);
    }
    frontmatter[m[1]] = value;
  }
  return { frontmatter, body: raw.slice(match[0].length) };
}

function allMarkdown(dir) {
  return fs
    .readdirSync(dir, { withFileTypes: true })
    .flatMap((entry) => {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) return allMarkdown(full);
      return entry.name.endsWith(".md") ? [full] : [];
    })
    .sort();
}

function outPath(rel, frontmatter = {}) {
  if (frontmatter.permalink) {
    const permalink = normalizePermalink(frontmatter.permalink);
    if (permalink === "/") return "index.html";
    return `${permalink.slice(1)}/index.html`;
  }
  if (rel === "index.md" || rel === "README.md") return "index.html";
  if (rel.endsWith("/README.md")) return rel.replace(/README\.md$/, "index.html");
  return rel.replace(/\.md$/, ".html");
}

function normalizePermalink(value) {
  let v = value.trim();
  if (!v.startsWith("/")) v = `/${v}`;
  if (v.length > 1 && v.endsWith("/")) v = v.slice(0, -1);
  return v || "/";
}

function firstHeading(markdown) {
  return markdown.match(/^#\s+(.+)$/m)?.[1]?.trim();
}

function titleize(input) {
  return input.replaceAll("-", " ").replace(/\b\w/g, (m) => m.toUpperCase());
}

function normalizeSection(section) {
  if (Array.isArray(section)) {
    const [name, rels] = section;
    const pages = rels.map((rel) => pageMap.get(rel)).filter(Boolean);
    return { name, groups: [{ name: "", pages }], pages };
  }
  const groups = section.groups
    .map((group) => ({ name: group.name, pages: group.pages.filter(Boolean) }))
    .filter((group) => group.pages.length);
  return { name: section.name, groups, pages: groups.flatMap((group) => group.pages) };
}

function serviceNavSection(servicePages) {
  const byGroup = new Map(serviceGroupOrder.map((name) => [name, []]));
  for (const page of servicePages) {
    const group = serviceGroupFor(page);
    if (!byGroup.has(group)) byGroup.set(group, []);
    byGroup.get(group).push(page);
  }
  return {
    name: "Services",
    groups: [...byGroup.entries()]
      .map(([name, pages]) => ({ name, pages: pages.sort((a, b) => a.title.localeCompare(b.title)) }))
      .filter((group) => group.pages.length),
  };
}

function serviceGroupFor(page) {
  if (page.frontmatter.service_group) return page.frontmatter.service_group;
  const slug = path.basename(page.rel, ".md");
  return serviceGroupsBySlug.get(slug) || "Internal";
}

function markdownToHtml(markdown, currentRel) {
  const lines = markdown.replace(/\r\n/g, "\n").split("\n");
  const html = [];
  let paragraph = [];
  let list = null;
  let fence = null;
  let table = null;
  let htmlComment = false;

  const flushParagraph = () => {
    if (!paragraph.length) return;
    html.push(`<p>${inline(paragraph.join(" "), currentRel)}</p>`);
    paragraph = [];
  };
  const closeList = () => {
    if (!list) return;
    html.push(`</${list}>`);
    list = null;
  };
  const flushTable = () => {
    if (!table) return;
    html.push('<div class="table-wrap"><table><thead><tr>');
    for (const cell of table.header) html.push(`<th>${inline(cell, currentRel)}</th>`);
    html.push("</tr></thead><tbody>");
    for (const row of table.rows) {
      html.push("<tr>");
      for (const cell of row) html.push(`<td>${inline(cell, currentRel)}</td>`);
      html.push("</tr>");
    }
    html.push("</tbody></table></div>");
    table = null;
  };

  for (let i = 0; i < lines.length; i++) {
    let line = lines[i];
    let strippedComment = false;
    const fenceMatch = line.match(/^```(.*)$/);
    if (fence) {
      if (fenceMatch) {
        html.push(codeBlock(fence.lang, fence.body.join("\n")));
        fence = null;
      } else {
        fence.body.push(line);
      }
      continue;
    }
    if (fenceMatch) {
      flushParagraph();
      closeList();
      flushTable();
      fence = { lang: fenceMatch[1].trim(), body: [] };
      continue;
    }
    if (htmlComment) {
      const end = line.indexOf("-->");
      if (end === -1) continue;
      htmlComment = false;
      strippedComment = true;
      line = line.slice(end + 3);
    }
    while (line.includes("<!--")) {
      const start = line.indexOf("<!--");
      const end = line.indexOf("-->", start + 4);
      strippedComment = true;
      if (end === -1) {
        htmlComment = true;
        line = line.slice(0, start);
        break;
      }
      line = `${line.slice(0, start)}${line.slice(end + 3)}`;
    }
    if (strippedComment && !line.trim()) continue;
    if (!line.trim()) {
      if (table && nextSignificantLineIsTableRow(lines, i + 1)) continue;
      flushParagraph();
      closeList();
      flushTable();
      continue;
    }
    const heading = line.match(/^(#{1,6})\s+(.+)$/);
    if (heading) {
      flushParagraph();
      closeList();
      flushTable();
      const level = heading[1].length;
      const text = heading[2].replace(/\s+#+\s*$/, "").trim();
      const id = slugify(text);
      html.push(`<h${level} id="${id}">${inline(text, currentRel)}</h${level}>`);
      continue;
    }
    if (isTableStart(lines, i)) {
      flushParagraph();
      closeList();
      const header = splitTableRow(line);
      table = { header, rows: [] };
      i++;
      continue;
    }
    if (table && /^\s*\|/.test(line)) {
      table.rows.push(splitTableRow(line));
      continue;
    }
    const unordered = line.match(/^\s*[-*]\s+(.+)$/);
    const ordered = line.match(/^\s*\d+\.\s+(.+)$/);
    if (unordered || ordered) {
      flushParagraph();
      flushTable();
      const kind = unordered ? "ul" : "ol";
      if (list && list !== kind) closeList();
      if (!list) {
        list = kind;
        html.push(`<${kind}>`);
      }
      html.push(`<li>${inline((unordered || ordered)[1], currentRel)}</li>`);
      continue;
    }
    if (line.startsWith("> ")) {
      flushParagraph();
      closeList();
      flushTable();
      html.push(`<blockquote><p>${inline(line.slice(2), currentRel)}</p></blockquote>`);
      continue;
    }
    paragraph.push(line.trim());
  }
  flushParagraph();
  closeList();
  flushTable();
  if (fence) html.push(codeBlock(fence.lang, fence.body.join("\n")));
  return html.join("\n");
}

function nextSignificantLineIsTableRow(lines, start) {
  let htmlComment = false;
  for (let i = start; i < lines.length; i++) {
    let line = lines[i];
    if (htmlComment) {
      const end = line.indexOf("-->");
      if (end === -1) continue;
      htmlComment = false;
      line = line.slice(end + 3);
    }
    while (line.includes("<!--")) {
      const commentStart = line.indexOf("<!--");
      const commentEnd = line.indexOf("-->", commentStart + 4);
      if (commentEnd === -1) {
        htmlComment = true;
        line = line.slice(0, commentStart);
        break;
      }
      line = `${line.slice(0, commentStart)}${line.slice(commentEnd + 3)}`;
    }
    if (!line.trim()) continue;
    return /^\s*\|/.test(line);
  }
  return false;
}

function codeBlock(rawLang, body) {
  const lang = normalizeLang(rawLang);
  const label = lang || "text";
  return `<div class="codeblock" data-lang="${escapeAttr(label)}"><div class="codebar"><span>${escapeHtml(label)}</span><button type="button" class="copy-code">Copy</button></div><pre><code class="language-${escapeAttr(label)}">${highlightCode(body, label)}</code></pre></div>`;
}

function normalizeLang(rawLang) {
  const lang = String(rawLang || "text").trim().toLowerCase().split(/\s+/)[0] || "text";
  if (["sh", "shell", "zsh", "console"].includes(lang)) return "bash";
  if (["js", "javascript"].includes(lang)) return "javascript";
  return lang;
}

function highlightCode(body, lang) {
  if (["bash", "text"].includes(lang)) return highlightShell(body);
  if (lang === "json") return highlightJson(body);
  return escapeHtml(body);
}

function highlightShell(body) {
  return body.split("\n").map((line) => {
    if (/^\s*#/.test(line)) return `<span class="tok-comment">${escapeHtml(line)}</span>`;
    let out = escapeHtml(line);
    out = out.replace(/(&quot;.*?&quot;|&#39;.*?&#39;)/g, '<span class="tok-string">$1</span>');
    out = out.replace(/(\s|^)(--?[a-zA-Z0-9][a-zA-Z0-9-]*)(?=\s|=|$)/g, '$1<span class="tok-flag">$2</span>');
    out = out.replace(/^(\s*)([A-Za-z_][A-Za-z0-9_.-]*)(?=\s|$)/, '$1<span class="tok-command">$2</span>');
    out = out.replace(/(\s)(\|)(\s)/g, '$1<span class="tok-pipe">$2</span>$3');
    return out;
  }).join("\n");
}

function highlightJson(body) {
  return escapeHtml(body).replace(/(&quot;[^&]*?&quot;)(\s*:)?|(-?\b\d+(?:\.\d+)?\b)|\b(true|false|null)\b/g, (match, str, colon, num, atom) => {
    if (str && colon) return `<span class="tok-key">${str}</span>${colon}`;
    if (str) return `<span class="tok-string">${str}</span>`;
    if (num) return `<span class="tok-number">${num}</span>`;
    if (atom) return `<span class="tok-atom">${atom}</span>`;
    return match;
  });
}

function isTableStart(lines, index) {
  return /^\s*\|/.test(lines[index] || "") && /^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$/.test(lines[index + 1] || "");
}

function splitTableRow(line) {
  let body = line.trim();
  if (body.startsWith("|")) body = body.slice(1);
  if (body.endsWith("|")) body = body.slice(0, -1);
  const cells = [];
  let cell = "";
  let inCode = false;
  for (let i = 0; i < body.length; i++) {
    const char = body[i];
    if (char === "\\" && body[i + 1] === "|") {
      cell += "|";
      i++;
      continue;
    }
    if (char === "`") inCode = !inCode;
    if (char === "|" && !inCode) {
      cells.push(cell.trim());
      cell = "";
      continue;
    }
    cell += char;
  }
  cells.push(cell.trim());
  return cells;
}

function inline(text, currentRel) {
  let out = escapeHtml(text);
  out = out.replace(/`([^`]+)`/g, "<code>$1</code>");
  out = out.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  out = out.replace(/!\[([^\]]*)\]\(([^)]+)\)/g, (_m, alt, href) => `<img alt="${escapeAttr(alt)}" src="${escapeAttr(rewriteLink(href, currentRel))}">`);
  out = out.replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_m, label, href) => `<a href="${escapeAttr(rewriteLink(href, currentRel))}">${label}</a>`);
  return out;
}

function rewriteLink(rawHref, currentRel) {
  const href = rawHref.trim().replace(/^<|>$/g, "");
  if (/^[a-z][a-z0-9+.-]*:/i.test(href) || href.startsWith("#")) return href;
  const [target, anchor] = href.split("#", 2);
  if (!target) return href;
  if (!target.endsWith(".md")) return href;
  const fromDir = path.posix.dirname(currentRel);
  const targetRel = path.posix.normalize(path.posix.join(fromDir === "." ? "" : fromDir, target));
  const page = pageMap.get(targetRel);
  const htmlRel = page ? page.outRel : targetRel.replace(/\.md$/, ".html");
  const fromOutDir = path.posix.dirname(pageMap.get(currentRel)?.outRel || currentRel.replace(/\.md$/, ".html"));
  let rel = path.posix.relative(fromOutDir === "." ? "" : fromOutDir, htmlRel) || path.posix.basename(htmlRel);
  if (!rel.startsWith(".")) rel = rel || "index.html";
  return anchor ? `${rel}#${anchor}` : rel;
}

function tocFromHtml(html) {
  const toc = [];
  const re = /<h([23]) id="([^"]+)">([\s\S]*?)<\/h\1>/g;
  let match;
  while ((match = re.exec(html))) {
    toc.push({ level: Number(match[1]), id: match[2], text: stripTags(match[3]) });
  }
  return toc.slice(0, 12);
}

function publicPath(outRel) {
  if (outRel === "index.html") return "/";
  return `/${outRel.replace(/index\.html$/, "")}`;
}

function absolutePageUrl(outRel) {
  return `${siteOrigin}${publicPath(outRel)}`;
}

function decorateArticleHtml(page, html) {
  if (page.outRel !== "index.html") return html;
  return html.replace(
    /^<p><img alt="gum logo" src="([^"]+)"><\/p>\n<h1 id="([^"]+)">([\s\S]*?)<\/h1>\n<p>([\s\S]*?)<\/p>/,
    '<section class="home-hero"><div class="home-hero-copy"><h1 id="$2">$3</h1><p>$4</p></div><figure class="home-hero-logo"><img alt="gum logo" src="$1"></figure></section>',
  );
}

function layout({ page, html, toc, prev, next, sectionName }) {
  const title = page.outRel === "index.html" ? `${productName} - ${productTagline}` : `${page.title} - ${productName}`;
  const description = page.frontmatter.description || productDescription;
  const pageUrl = absolutePageUrl(page.outRel);
  const articleHtml = decorateArticleHtml(page, html);
  const primaryLinks = `<a href="${relativeHref(page.outRel, "services/index.html")}">Services</a><a href="${relativeHref(page.outRel, "commands/index.html")}">Commands</a><a href="${relativeHref(page.outRel, "quickstart.html")}">Quickstart</a><a href="${repoBase}">Source</a>`;
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<script>document.documentElement.classList.add("js")</script>
<title>${escapeHtml(title)}</title>
<meta name="description" content="${escapeAttr(description)}">
<link rel="canonical" href="${escapeAttr(pageUrl)}">
<meta name="theme-color" content="#070b09">
<meta property="og:type" content="website">
<meta property="og:site_name" content="${escapeAttr(productName)}">
<meta property="og:title" content="${escapeAttr(title)}">
<meta property="og:description" content="${escapeAttr(description)}">
<meta property="og:url" content="${escapeAttr(pageUrl)}">
<meta property="og:image" content="${escapeAttr(socialImage)}">
<meta property="og:image:secure_url" content="${escapeAttr(socialImage)}">
<meta property="og:image:type" content="image/png">
<meta property="og:image:width" content="1200">
<meta property="og:image:height" content="630">
<meta property="og:image:alt" content="gum documentation card: Google APIs for agents and terminals">
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:domain" content="gumcli.dev">
<meta name="twitter:url" content="${escapeAttr(pageUrl)}">
<meta name="twitter:title" content="${escapeAttr(title)}">
<meta name="twitter:description" content="${escapeAttr(description)}">
<meta name="twitter:image" content="${escapeAttr(socialImage)}">
<meta name="twitter:image:src" content="${escapeAttr(socialImage)}">
<meta name="twitter:image:alt" content="gum documentation card: Google APIs for agents and terminals">
<link rel="icon" type="image/png" href="${relativeHref(page.outRel, "assets/gum-icon.png")}">
<link rel="apple-touch-icon" href="${relativeHref(page.outRel, "assets/gum-icon.png")}">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=B612:ital,wght@0,400;0,700;1,400&family=B612+Mono:wght@400;700&family=Commissioner:wght@500;650;800&display=swap" rel="stylesheet">
<style>${css()}</style>
</head>
<body${page.outRel === "index.html" ? ' class="home"' : ""}>
<header class="top">
  <button class="nav-burger" type="button" aria-label="Menu" aria-controls="site-nav" aria-expanded="false"><span></span><span></span><span></span></button>
  <a class="brand" href="${relativeToRoot(page.outRel)}"><img src="${relativeHref(page.outRel, "assets/gum-wordmark.png")}" alt="gum"><span>field manual</span></a>
  <nav class="top-links">${primaryLinks}</nav>
</header>
<div class="nav-scrim"></div>
<div class="shell">
  <aside class="sidebar" id="site-nav"><div class="drawer-head"><span>Menu</span><button class="drawer-close" type="button" aria-label="Close menu">&times;</button></div><nav class="sidebar-quicklinks">${primaryLinks}</nav><label class="nav-search"><span>Search docs</span><input type="search" id="nav-filter" autocomplete="off" placeholder="Filter pages"></label>${navHtml(page)}</aside>
  <main>
    <div class="manual-head"><span>${escapeHtml(sectionName)}</span><span>${escapeHtml(page.title)}</span><span>${productTagline}</span></div>
    ${toc.length ? `<nav class="page-map" aria-label="On this page">${toc.map((item) => `<a class="l${item.level}" href="#${item.id}">${escapeHtml(item.text)}</a>`).join("")}</nav>` : ""}
    <article>${articleHtml}</article>
    <nav class="pager">${prev ? `<a href="${relativeHref(page.outRel, prev.outRel)}">&larr; ${escapeHtml(prev.title)}</a>` : "<span></span>"}${next ? `<a href="${relativeHref(page.outRel, next.outRel)}">${escapeHtml(next.title)} &rarr;</a>` : "<span></span>"}</nav>
  </main>
</div>
<script>${clientJs()}</script>
</body>
</html>
`;
}

function navHtml(current) {
  return nav.map((section) => {
    const groups = section.groups.map((group) => {
      const links = group.pages.map((page) => {
        const active = page.rel === current.rel ? " active" : "";
        return `<a class="${active}" href="${relativeHref(current.outRel, page.outRel)}">${escapeHtml(page.title)}</a>`;
      }).join("");
      if (!group.name) return links;
      return `<div class="nav-subgroup"><h3>${escapeHtml(group.name)}</h3>${links}</div>`;
    }).join("");
    return `<section><h2>${escapeHtml(section.name)}</h2>${groups}</section>`;
  }).join("");
}

function llmsTxt() {
  const lines = [
    `# ${productName}`,
    "",
    productDescription,
    "",
    "Canonical documentation:",
    ...orderedPages.map((page) => `- ${page.title}: ${page.outRel === "index.html" ? "/" : `/${page.outRel}`}`),
    "",
    "Install:",
    `- ${installCommand}`,
    "",
    `Source: ${repoBase}`,
    "",
    "Guidance for agents:",
    "- Fetch only the pages needed for the current task.",
    "- Prefer command pages generated from `gum schema --json` when checking flags or subcommands.",
  ];
  return `${lines.join("\n")}\n`;
}

function validateLinks(dir) {
  const htmlFiles = listFiles(dir).filter((file) => file.endsWith(".html"));
  for (const file of htmlFiles) {
    const html = fs.readFileSync(file, "utf8");
    for (const match of html.matchAll(/href="([^"]+)"/g)) {
      const href = match[1];
      if (/^[a-z][a-z0-9+.-]*:/i.test(href) || href.startsWith("#")) continue;
      const target = href.split("#")[0];
      if (!target) continue;
      const resolved = path.resolve(path.dirname(file), target);
      if (!fs.existsSync(resolved)) {
        throw new Error(`broken link in ${path.relative(root, file)}: ${href}`);
      }
    }
  }
}

function listFiles(dir) {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(dir, entry.name);
    return entry.isDirectory() ? listFiles(full) : [full];
  });
}

function copyStaticDir(src, dest) {
  if (!fs.existsSync(src)) return;
  fs.mkdirSync(dest, { recursive: true });
  for (const entry of fs.readdirSync(src, { withFileTypes: true })) {
    const from = path.join(src, entry.name);
    const to = path.join(dest, entry.name);
    if (entry.isDirectory()) copyStaticDir(from, to);
    else fs.copyFileSync(from, to);
  }
}

function relativeToRoot(outRel) {
  return relativeHref(outRel, "index.html");
}

function relativeHref(from, to) {
  const fromDir = path.posix.dirname(from);
  let rel = path.posix.relative(fromDir === "." ? "" : fromDir, to);
  if (!rel) rel = "index.html";
  return rel;
}

function slugify(text) {
  return stripTags(text)
    .toLowerCase()
    .replace(/`/g, "")
    .replace(/[^a-z0-9\s-]/g, "")
    .trim()
    .replace(/\s+/g, "-");
}

function stripTags(value) {
  return value.replace(/<[^>]+>/g, "");
}

function escapeHtml(value) {
  return String(value ?? "").replace(/[&<>"']/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[char]);
}

function escapeAttr(value) {
  return escapeHtml(value).replace(/`/g, "&#96;");
}

function clientJs() {
  return `
const filter = document.querySelector("#nav-filter");
filter?.addEventListener("input", () => {
  const query = filter.value.trim().toLowerCase();
  document.querySelectorAll(".sidebar section").forEach((section) => {
    let visible = false;
    section.querySelectorAll("a").forEach((link) => {
      const hit = !query || link.textContent.toLowerCase().includes(query);
      link.hidden = !hit;
      visible ||= hit;
    });
    section.querySelectorAll(".nav-subgroup").forEach((group) => {
      const groupVisible = [...group.querySelectorAll("a")].some((link) => !link.hidden);
      group.hidden = !groupVisible;
    });
    section.hidden = !visible;
  });
});
document.querySelectorAll(".copy-code").forEach((button) => {
  button.addEventListener("click", async () => {
    const code = button.closest(".codeblock")?.querySelector("code")?.textContent || "";
    await navigator.clipboard.writeText(code);
    button.textContent = "Copied";
    setTimeout(() => { button.textContent = "Copy"; }, 1200);
  });
});
const navBurger = document.querySelector(".nav-burger");
const navSidebar = document.getElementById("site-nav");
const navScrim = document.querySelector(".nav-scrim");
const navClose = document.querySelector(".drawer-close");
function setNav(open) {
  document.body.classList.toggle("nav-open", open);
  navBurger?.setAttribute("aria-expanded", open ? "true" : "false");
}
navBurger?.addEventListener("click", () => setNav(!document.body.classList.contains("nav-open")));
navScrim?.addEventListener("click", () => setNav(false));
navClose?.addEventListener("click", () => setNav(false));
navSidebar?.querySelectorAll("a").forEach((link) => link.addEventListener("click", () => setNav(false)));
document.addEventListener("keydown", (event) => { if (event.key === "Escape") setNav(false); });
`;
}

function css() {
  return `
:root{color-scheme:dark;--ink:#eef5ee;--text:#cfddd3;--muted:#899991;--faint:#56645d;--rule:#26342e;--rule-strong:#42524a;--paper:#070b09;--wash:#0c1210;--panel:#101713;--panel-2:#151d18;--accent:#7ee0bf;--accent-dim:#2f806b;--copper:#d6a46d;--blue:#83b7ff;--violet:#c59bff;--code:#050807;--code-line:#1f2a25;--code-border:#33443c;--code-muted:#83968d;--code-command:#92f0ce;--code-flag:#f1c270;--code-string:#a6dc74;--code-key:#8dbdff;--code-number:#ee996f}
*{box-sizing:border-box}
body{margin:0;background:var(--paper);color:var(--text);font:16px/1.58 "B612",Avenir Next,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
body:before{content:"";position:fixed;inset:0;z-index:-1;background:linear-gradient(90deg,rgba(126,224,191,.045) 1px,transparent 1px),linear-gradient(180deg,rgba(126,224,191,.035) 1px,transparent 1px);background-size:80px 80px;mask-image:linear-gradient(180deg,rgba(0,0,0,.55),rgba(0,0,0,.08) 62%,transparent);pointer-events:none}
a{color:var(--accent);text-decoration-thickness:1px;text-underline-offset:3px}a:hover{color:var(--ink)}
a:focus-visible,button:focus-visible,input:focus-visible{outline:1px solid var(--accent);outline-offset:3px}
.top{position:sticky;top:0;z-index:3;display:flex;align-items:center;justify-content:space-between;gap:24px;max-width:1540px;margin:0 auto;padding:18px 34px;border-bottom:1px solid var(--rule-strong);background:color-mix(in srgb,var(--paper) 92%,transparent);backdrop-filter:blur(18px)}
.brand{display:grid;grid-template-columns:auto auto;align-items:end;gap:14px;color:var(--ink);font-family:"Commissioner","B612",sans-serif;font-weight:800;text-decoration:none}.brand:hover{text-decoration:none}.brand img{width:128px;height:auto;display:block}.brand span{padding-bottom:3px;color:var(--muted);font-size:11px;line-height:1;text-transform:uppercase;letter-spacing:.14em}
.top nav{display:flex;align-items:center;gap:0;border:1px solid var(--rule);background:var(--wash);font-family:"Commissioner","B612",sans-serif;font-size:12px;font-weight:650;text-transform:uppercase;letter-spacing:.08em}.top nav a{display:inline-flex;align-items:center;min-height:36px;border-right:1px solid var(--rule);color:var(--muted);padding:6px 13px;text-decoration:none}.top nav a:last-child{border-right:0}.top nav a:hover{background:var(--panel);color:var(--ink)}
.copy-code{font:inherit;font-size:12px;cursor:pointer}
.nav-burger{display:none;flex-direction:column;justify-content:center;gap:4px;width:42px;height:40px;flex:none;padding:9px 8px;border:1px solid var(--rule-strong);background:var(--wash);cursor:pointer}.nav-burger span{display:block;height:1.5px;background:var(--ink)}.nav-burger:hover{background:var(--panel)}
.nav-scrim{display:none}
.drawer-head,.sidebar-quicklinks{display:none}
.shell{display:grid;grid-template-columns:330px minmax(0,980px);gap:58px;max-width:1540px;margin:0 auto;padding:46px 34px 64px}
main{min-width:0}.manual-head{display:grid;grid-template-columns:max-content minmax(0,1fr) max-content;gap:12px;align-items:center;margin:0 0 22px;padding:8px 0;border-top:1px solid var(--rule);border-bottom:1px solid var(--rule);color:var(--muted);font-family:"Commissioner","B612",sans-serif;font-size:11px;font-weight:650;text-transform:uppercase;letter-spacing:.1em}.manual-head span{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.manual-head span+span:before{content:"/";margin-right:12px;color:var(--faint)}
.page-map{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:0;margin:0 0 32px;border:1px solid var(--rule);background:var(--wash)}.page-map a{display:flex;align-items:center;min-height:42px;border-right:1px solid var(--rule);border-bottom:1px solid var(--rule);color:var(--muted);font-family:"Commissioner","B612",sans-serif;font-size:12px;font-weight:650;line-height:1.25;padding:9px 11px;text-decoration:none;text-transform:uppercase;letter-spacing:.06em}.page-map a:hover{background:var(--panel);color:var(--ink)}.page-map .l3{color:var(--faint)}
.sidebar{align-self:start;border:1px solid var(--rule);background:rgba(12,18,16,.82)}.sidebar section{border-top:1px solid var(--rule)}.sidebar section:first-of-type{border-top:0}.sidebar h2{margin:0;padding:10px 14px;border-bottom:1px solid var(--rule);background:var(--panel-2);color:var(--copper);font-family:"Commissioner","B612",sans-serif;font-size:11px;font-weight:650;letter-spacing:.11em;text-transform:uppercase}.nav-subgroup{border-top:1px solid var(--rule)}.nav-subgroup:first-of-type{border-top:0}.nav-subgroup h3{margin:0;padding:8px 14px;border-bottom:1px solid rgba(66,82,74,.6);background:rgba(21,29,24,.52);color:var(--muted);font-family:"Commissioner","B612",sans-serif;font-size:10px;font-weight:650;letter-spacing:.1em;text-transform:uppercase}.sidebar a{display:block;padding:6px 14px;border-top:1px solid rgba(66,82,74,.45);color:var(--muted);font-size:13px;line-height:1.35;text-decoration:none}.sidebar a:first-of-type{border-top:0}.sidebar a:hover{background:var(--panel);color:var(--ink)}.sidebar a.active{background:var(--accent-dim);color:var(--ink);font-weight:700}.nav-search{display:block;padding:14px;border-bottom:1px solid var(--rule)}.nav-search span{display:block;margin-bottom:7px;color:var(--muted);font-family:"Commissioner","B612",sans-serif;font-size:11px;font-weight:650;text-transform:uppercase;letter-spacing:.11em}.nav-search input{width:100%;border:1px solid var(--rule-strong);border-radius:0;background:var(--code);color:var(--ink);padding:9px 10px;font:inherit;font-size:14px}.nav-search input::placeholder{color:var(--faint)}
article{min-width:0;max-width:100%;overflow-x:hidden;background:transparent}article>p:first-child img[alt="gum logo"]{width:min(100%,360px);margin:10px 0 8px}.home-hero{display:grid;grid-template-columns:minmax(0,1fr) minmax(270px,420px);gap:44px;align-items:center;margin:2px 0 36px}.home-hero-copy{min-width:0}.home-hero-copy h1{margin-bottom:18px}.home-hero-copy p{margin:0;color:var(--text);font-size:18px;line-height:1.6}.home-hero-logo{margin:0;justify-self:end}.home-hero-logo img{display:block;width:min(100%,420px);height:auto}h1,h2,h3{color:var(--ink);font-family:"B612","Commissioner",sans-serif;font-weight:700}h1{margin:0 0 22px;font-size:clamp(36px,3.9vw,56px);line-height:1.04;letter-spacing:0;max-width:17ch}h2{margin:50px 0 15px;padding-top:10px;border-top:1px solid var(--rule-strong);font-size:25px;line-height:1.18;letter-spacing:0}h3{margin:31px 0 10px;font-size:18px;line-height:1.25;letter-spacing:0}p{margin:14px 0;max-width:73ch}strong{color:var(--ink)}ul,ol{padding-left:24px;max-width:78ch}li{margin:7px 0}hr{border:0;border-top:1px solid var(--rule);margin:34px 0}
.codeblock{margin:24px 0;border:1px solid var(--code-border);background:var(--code);overflow:hidden}.codebar{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:8px 12px;border-bottom:1px solid var(--code-line);background:#0a0f0d;color:var(--code-muted);font-family:"Commissioner","B612",sans-serif;font-size:11px;font-weight:650;text-transform:uppercase;letter-spacing:.1em}.copy-code{border:1px solid var(--code-line);border-radius:0;background:transparent;color:var(--code-muted);padding:4px 9px;text-transform:none;letter-spacing:0}.copy-code:hover{border-color:var(--accent);color:var(--accent)}.codeblock pre{overflow:visible;margin:0;padding:18px 20px;background:transparent;color:#e6fff7;white-space:pre-wrap}.codeblock code{white-space:pre-wrap;overflow-wrap:anywhere;word-break:break-word}code{font-family:"B612 Mono",ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:.9em}p code,li code,td code{padding:2px 5px;border:1px solid var(--rule);border-radius:0;background:var(--panel);color:var(--ink)}
.tok-comment{color:var(--code-muted)}.tok-command{color:var(--code-command);font-weight:700}.tok-flag,.tok-pipe{color:var(--code-flag)}.tok-string{color:var(--code-string)}.tok-key{color:var(--code-key)}.tok-number{color:var(--code-number)}.tok-atom{color:#d7a8ff}
.table-wrap{width:100%;max-width:100%;overflow-x:auto;margin:22px 0;border:1px solid var(--rule);background:var(--wash)}table{width:100%;min-width:720px;table-layout:fixed;border-collapse:collapse;margin:0;border:0;font-size:14px;background:var(--wash)}th,td{padding:10px 12px;border:1px solid var(--rule);vertical-align:top;overflow-wrap:anywhere}th{background:var(--panel-2);color:var(--ink);font-family:"Commissioner","B612",sans-serif;font-size:12px;font-weight:650;text-align:left;text-transform:uppercase;letter-spacing:.06em}td code{white-space:normal}
blockquote{margin:20px 0;padding:12px 16px;border:1px solid var(--rule);background:var(--wash);color:var(--muted)}
img{max-width:100%;height:auto}.pager{display:grid;grid-template-columns:1fr 1fr;gap:20px;margin:54px 0 24px;padding-top:20px;border-top:1px solid var(--rule-strong)}.pager a{display:block;border:1px solid var(--rule);background:var(--wash);padding:12px 14px;text-decoration:none}.pager a:last-child{text-align:right}.pager a:hover{background:var(--panel);color:var(--ink)}
@media (max-width:1100px){.shell{grid-template-columns:minmax(0,1fr);gap:32px}.sidebar{max-width:100%}html.js .nav-burger{display:flex}html.js .top{justify-content:flex-start;gap:16px}html.js .top-links{display:none}html.js .sidebar{position:fixed;top:0;left:0;z-index:20;height:100vh;height:100dvh;width:min(86vw,340px);max-width:none;margin:0;border:0;border-right:1px solid var(--rule-strong);background:var(--panel);overflow-y:auto;transform:translateX(-100%);visibility:hidden;transition:transform .22s ease,visibility .22s}html.js body.nav-open .sidebar{transform:translateX(0);visibility:visible}html.js .drawer-head{display:flex;align-items:center;justify-content:space-between;padding:13px 14px;border-bottom:1px solid var(--rule-strong);color:var(--muted);font-family:"Commissioner","B612",sans-serif;font-size:11px;font-weight:650;text-transform:uppercase;letter-spacing:.11em}html.js .drawer-close{width:30px;height:30px;border:1px solid var(--rule-strong);background:var(--wash);color:var(--ink);font-size:18px;line-height:1;cursor:pointer}html.js .sidebar-quicklinks{display:grid;grid-template-columns:1fr 1fr;border-bottom:1px solid var(--rule)}html.js .sidebar-quicklinks a{display:flex;align-items:center;min-height:40px;padding:8px 14px;border-top:1px solid var(--rule);border-right:1px solid var(--rule);color:var(--muted);font-family:"Commissioner","B612",sans-serif;font-size:12px;font-weight:650;text-transform:uppercase;letter-spacing:.06em;text-decoration:none}html.js .sidebar-quicklinks a:nth-child(2n){border-right:0}html.js .sidebar-quicklinks a:hover{background:var(--panel);color:var(--ink)}html.js .nav-scrim{display:block;position:fixed;inset:0;z-index:15;background:rgba(0,0,0,.58);opacity:0;pointer-events:none;transition:opacity .22s}html.js body.nav-open .nav-scrim{opacity:1;pointer-events:auto}html.js body.nav-open{overflow:hidden}}
@media (max-width:760px){.top{align-items:center;flex-direction:row;flex-wrap:wrap;gap:12px;padding:12px 16px}.brand img{width:104px}.shell{display:block;padding:20px 18px}.top nav{width:100%;display:grid;grid-template-columns:1fr 1fr}.top nav a{border-bottom:1px solid var(--rule);min-height:36px}.top nav a:nth-child(2n){border-right:0}.manual-head{display:none}.page-map{display:none}article>p:first-child img[alt="gum logo"]{width:min(100%,310px)}.home-hero{grid-template-columns:1fr;gap:20px;margin-top:0}.home-hero-logo{justify-self:start;order:-1}.home-hero-logo img{width:min(100%,310px)}.home-hero-copy p{font-size:16px}h1{font-size:38px;max-width:none}.codeblock{margin-left:-8px;margin-right:-8px}.pager{grid-template-columns:1fr}.pager a:last-child{text-align:left}}
`;
}
