#!/usr/bin/env node
import fs from "node:fs";
import path from "node:path";

const root = process.cwd();
const catalogPath = path.join(root, "apps", "gum", "internal", "embedded", "catalog.json");
const docsDir = path.join(root, "docs");
const servicesDir = path.join(docsDir, "services");

const catalog = JSON.parse(fs.readFileSync(catalogPath, "utf8"));
const ops = Array.isArray(catalog.ops) ? catalog.ops : [];

const labels = new Map([
  ["admin", "Admin SDK"],
  ["adminreports", "Admin Reports"],
  ["calendar", "Calendar"],
  ["chat", "Chat"],
  ["classroom", "Classroom"],
  ["cloudidentity", "Cloud Identity"],
  ["customsearch", "Custom Search"],
  ["docs", "Docs"],
  ["drive", "Drive"],
  ["flights", "Flights"],
  ["forms", "Forms"],
  ["gmail", "Gmail"],
  ["googleads", "Google Ads"],
  ["groupssettings", "Groups Settings"],
  ["gum", "gum"],
  ["indexing", "Indexing"],
  ["maps", "Maps"],
  ["meet", "Meet"],
  ["meta", "Sandbox"],
  ["patents", "Patents"],
  ["people", "People"],
  ["photoslibrary", "Photos Library"],
  ["places", "Places"],
  ["routes", "Routes"],
  ["scholar", "Scholar"],
  ["script", "Apps Script"],
  ["searchconsole", "Search Console"],
  ["sheets", "Sheets"],
  ["slides", "Slides"],
  ["tasks", "Tasks"],
  ["trends", "Trends"],
  ["vault", "Vault"],
  ["youtube", "YouTube"],
]);

const categoryOrder = [
  "Workspace documents",
  "Workspace communication",
  "Workspace administration",
  "People and education",
  "Search and media",
  "Ads and maps",
  "Research and travel",
  "Internal",
];
const categories = new Map([
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

const authGuideByService = new Map([
  ["admin", "../auth-guides/admin-cloud-vault.md"],
  ["adminreports", "../auth-guides/admin-cloud-vault.md"],
  ["calendar", "../auth-guides/calendar.md"],
  ["chat", "../auth-guides/chat.md"],
  ["classroom", "../auth-guides/classroom-forms-meet-script.md"],
  ["cloudidentity", "../auth-guides/admin-cloud-vault.md"],
  ["customsearch", "../auth-guides/maps-custom-search.md"],
  ["docs", "../auth-guides/docs-sheets-slides.md"],
  ["drive", "../auth-guides/drive.md"],
  ["forms", "../auth-guides/classroom-forms-meet-script.md"],
  ["gmail", "../auth-guides/gmail.md"],
  ["googleads", "../auth-guides/google-ads.md"],
  ["groupssettings", "../auth-guides/admin-cloud-vault.md"],
  ["indexing", "../auth-guides/README.md"],
  ["maps", "../auth-guides/maps-custom-search.md"],
  ["meet", "../auth-guides/classroom-forms-meet-script.md"],
  ["people", "../auth-guides/people.md"],
  ["photoslibrary", "../auth-guides/photos-library.md"],
  ["places", "../auth-guides/maps-custom-search.md"],
  ["routes", "../auth-guides/maps-custom-search.md"],
  ["script", "../auth-guides/classroom-forms-meet-script.md"],
  ["searchconsole", "../auth-guides/search-console.md"],
  ["sheets", "../auth-guides/docs-sheets-slides.md"],
  ["slides", "../auth-guides/docs-sheets-slides.md"],
  ["tasks", "../auth-guides/tasks.md"],
  ["vault", "../auth-guides/admin-cloud-vault.md"],
  ["youtube", "../auth-guides/youtube.md"],
]);

const apiEnableNameByService = new Map([
  ["admin", "Admin SDK API"],
  ["adminreports", "Admin Reports API"],
  ["calendar", "Google Calendar API"],
  ["chat", "Google Chat API"],
  ["classroom", "Google Classroom API"],
  ["cloudidentity", "Cloud Identity API"],
  ["customsearch", "Custom Search API"],
  ["docs", "Google Docs API"],
  ["drive", "Google Drive API"],
  ["forms", "Google Forms API"],
  ["gmail", "Gmail API"],
  ["googleads", "Google Ads API"],
  ["groupssettings", "Groups Settings API"],
  ["indexing", "Indexing API"],
  ["maps", "the required Maps Platform APIs"],
  ["meet", "Google Meet API"],
  ["people", "People API"],
  ["photoslibrary", "Photos Library API"],
  ["places", "Places API"],
  ["routes", "Routes API"],
  ["script", "Apps Script API"],
  ["searchconsole", "Search Console API"],
  ["sheets", "Google Sheets API"],
  ["slides", "Google Slides API"],
  ["tasks", "Google Tasks API"],
  ["vault", "Google Vault API"],
  ["youtube", "YouTube Data API v3"],
]);

const grouped = new Map();
for (const op of ops) {
  const service = String(op.service || op.op_id?.split(".")[0] || "other");
  if (!grouped.has(service)) grouped.set(service, []);
  grouped.get(service).push(op);
}

fs.rmSync(servicesDir, { recursive: true, force: true });
fs.mkdirSync(servicesDir, { recursive: true });
fs.writeFileSync(path.join(servicesDir, "README.md"), indexPage(), "utf8");

for (const [service, serviceOps] of sortedServices()) {
  fs.writeFileSync(path.join(servicesDir, `${slug(service)}.md`), servicePage(service, serviceOps), "utf8");
}

console.log(`generated ${grouped.size} service pages in ${path.relative(root, servicesDir)}`);

function indexPage() {
  const lines = [
    "---",
    "title: Operations by service",
    "description: \"Generated service pages for the Google API and plugin operations in gum.\"",
    "---",
    "",
    "# Operations by service",
    "",
    `gum ships ${ops.length} catalog operations across ${grouped.size} services. The CLI does not expose one top-level command per Google product; product coverage lives in the catalog. Use these pages to start from a service, then call the selected operation with \`gum read\`, \`gum write\`, or \`gum destructive\`.`,
    "",
    "```bash",
    "gum search \"gmail messages\"",
    "gum describe gmail.users.messages.list",
    "gum read gmail.users.messages.list --args '{\"userId\":\"me\",\"maxResults\":5}' --output json",
    "```",
    "",
  ];

  for (const category of categoryOrder) {
    const services = sortedServices().filter(([service]) => categoryFor(service) === category);
    if (!services.length) continue;
    lines.push(`## ${category}`, "");
    for (const [service, serviceOps] of services) {
      const risk = riskSummary(serviceOps);
      lines.push(`- [${label(service)}](${slug(service)}.md) - ${opCount(serviceOps.length)}; ${risk}.`);
    }
    lines.push("");
  }

  lines.push("## How to read these pages", "");
  lines.push("- `Risk` is the variant risk class gum enforces at dispatch time.");
  lines.push("- `Auth` is the credential strategy required by the default variant.");
  lines.push("- Request fields are listed on individual operation descriptions from `gum describe <op_id>`.");
  lines.push("- Google project setup still matters. Enable the API and authorize scopes before calling an operation.");
  lines.push("");
  return lines.join("\n");
}

function servicePage(service, serviceOps) {
  const serviceLabel = label(service);
  const family = categoryFor(service);
  const sortedOps = [...serviceOps].sort((a, b) => a.op_id.localeCompare(b.op_id));
  const risks = countBy(sortedOps, (op) => firstVariant(op).risk_class || "unknown");
  const auth = countBy(sortedOps, (op) => firstVariant(op).auth_strategy || "unknown");
  const first = sortedOps[0];
  const read = sortedOps.find((op) => firstVariant(op).risk_class === "read") || first;
  const write = sortedOps.find((op) => firstVariant(op).risk_class === "write");
  const destructive = sortedOps.find((op) => firstVariant(op).risk_class === "destructive");
  const lines = [
    "---",
    `title: ${serviceLabel}`,
    `description: \"${serviceLabel} operations in gum's generated catalog.\"`,
    `service_group: \"${family}\"`,
    "---",
    "",
    `# ${serviceLabel}`,
    "",
    `${serviceLabel} has ${sortedOps.length} operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.`,
    "",
    "| Count | Value |",
    "| --- | --- |",
    `| Family | ${family} |`,
    `| Operations | ${sortedOps.length} |`,
    `| Risk classes | ${mapSummary(risks)} |`,
    `| Auth strategies | ${mapSummary(auth)} |`,
    "",
    "## Start here",
    "",
    "```bash",
    `gum search "${searchQuery(service)}"`,
    `gum describe ${read.op_id}`,
    `${commandFor(read)} ${read.op_id} --args '${sampleArgs(read)}' --output json`,
    "```",
    "",
  ];

  if (write) {
    lines.push("For write-class operations, gum requires the write command and an explicit write gate:", "");
    lines.push("```bash", `gum describe ${write.op_id}`, `gum write ${write.op_id} --allow-write --args '${sampleArgs(write)}'`, "```", "");
  }
  if (destructive) {
    lines.push("For destructive operations, run the call once for a confirmation envelope, review the target, then retry with the returned token:", "");
    lines.push("```bash", `gum destructive ${destructive.op_id} --args '${sampleArgs(destructive)}'`, `gum destructive ${destructive.op_id} --args '${sampleArgs(destructive)}' --confirmed --token '<confirmation_token>'`, "```", "");
  }

  lines.push(...authSection(service, sortedOps));

  lines.push("## Operations", "");
  lines.push("| Operation | Risk | Auth | Summary |");
  lines.push("| --- | --- | --- | --- |");
  for (const op of sortedOps) {
    const variant = firstVariant(op);
    lines.push(`| \`${op.op_id}\` | \`${variant.risk_class || ""}\` | \`${variant.auth_strategy || ""}\` | ${mdEscape(op.summary || op.title || "")} |`);
  }
  lines.push("");
  lines.push("## Next", "");
  lines.push("- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.");
  lines.push("- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.");
  lines.push("- Use [Command index](../commands/README.md) for CLI flags and generated help.");
  lines.push("");
  return lines.join("\n");
}

function authSection(service, sortedOps) {
  const serviceLabel = label(service);
  const byStrategy = new Map();
  for (const op of sortedOps) {
    const variant = firstVariant(op);
    const strategy = variant.auth_strategy || "unknown";
    if (!byStrategy.has(strategy)) byStrategy.set(strategy, []);
    byStrategy.get(strategy).push(op);
  }

  const lines = ["## Auth", ""];
  lines.push(`Auth strategies in this service: ${mapSummary(countBy(sortedOps, (op) => firstVariant(op).auth_strategy || "unknown"))}. Authenticate the strategy used by the operation you plan to call.`);
  lines.push("");

  for (const [strategy, strategyOps] of [...byStrategy.entries()].sort((a, b) => a[0].localeCompare(b[0]))) {
    lines.push(`### ${authStrategyLabel(strategy)}`, "");
    if (strategy === "byo_oauth") lines.push(...byoOauthSteps(service, strategyOps));
    else if (strategy === "api_key") lines.push(...apiKeySteps(service, strategyOps));
    else if (strategy === "plugin_managed") lines.push(...pluginManagedSteps(strategyOps));
    else if (strategy === "none") lines.push(...noAuthSteps(strategyOps));
    else lines.push(...unknownAuthSteps(strategy, strategyOps));
    lines.push("");
  }

  return lines;
}

function byoOauthSteps(service, strategyOps) {
  const guide = authGuideByService.get(service) || "../auth-guides/README.md";
  const verifyOp = strategyOps.find((op) => firstVariant(op).risk_class === "read") || strategyOps[0];
  const scopes = uniqueScopes(strategyOps);
  const scopeArg = scopes.map(shortScope).join(",");
  const stepOffset = service === "googleads" ? 2 : 1;
  const lines = [
    `1. In Google Cloud, enable ${apiEnableNameByService.get(service) || `${label(service)} API`}.`,
    "2. Configure the OAuth consent screen. Add your Google account as a test user when the app is still in testing mode.",
    "3. Create an OAuth client ID with application type `Desktop app`.",
    "4. Add the scopes this service needs to the consent screen.",
    "5. Store the client in gum:",
    "",
    "```bash",
    "printf '%s' \"$GOOGLE_OAUTH_CLIENT_SECRET\" \\",
    "  | gum auth use-oauth-client --client-id \"$GOOGLE_OAUTH_CLIENT_ID\" --secret-stdin",
    "```",
    "",
  ];

  if (service === "googleads") {
    lines.push("6. Store the Google Ads developer token:", "", "```bash", "printf '%s' \"$GOOGLE_ADS_DEVELOPER_TOKEN\" | gum auth use-ads-developer-token --stdin", "```", "");
  }

  lines.push(`${5 + stepOffset}. Authorize this service:`, "", "```bash", `gum login --service ${service}`, "```", "");
  lines.push(`${6 + stepOffset}. Verify the grant before dispatch:`, "", "```bash", `gum auth status --scopes ${scopeArg || "gmail.readonly"}`, `gum describe ${verifyOp.op_id}`, "```", "");

  if (scopes.length) {
    lines.push("Scopes used by these operations:", "");
    for (const scope of scopes) lines.push(`- \`${scope}\``);
    lines.push("");
  }
  lines.push(`Service setup notes: [${authGuideLabel(service)}](${guide}).`);
  return lines;
}

function apiKeySteps(service, strategyOps) {
  const guide = authGuideByService.get(service) || "../auth-guides/README.md";
  const verifyOp = strategyOps[0];
  return [
    `1. In Google Cloud, enable ${apiEnableNameByService.get(service) || `${label(service)} API`}.`,
    "2. Create an API key. Restrict it to the API and to the hosts or referrers that should use it.",
    "3. Store the key in gum:",
    "",
    "```bash",
    "printf '%s' \"$GOOGLE_API_KEY\" | gum auth use-api-key --stdin",
    "```",
    "",
    "4. Verify with a read operation:",
    "",
    "```bash",
    `gum describe ${verifyOp.op_id}`,
    `${commandFor(verifyOp)} ${verifyOp.op_id} --args '${sampleArgs(verifyOp)}' --output json`,
    "```",
    "",
    `Service setup notes: [${authGuideLabel(service)}](${guide}).`,
  ];
}

function pluginManagedSteps(strategyOps) {
  const verifyOp = strategyOps[0];
  return [
    "1. No Google OAuth client, API key, or service account is configured through gum for these operations.",
    "2. Confirm the plugin-backed operation is available:",
    "",
    "```bash",
    "gum plugin list",
    `gum describe ${verifyOp.op_id}`,
    "```",
    "",
    "3. Follow the plugin's upstream requirements, rate limits, and terms before calling it.",
    "4. Verify with a read call:",
    "",
    "```bash",
    `${commandFor(verifyOp)} ${verifyOp.op_id} --args '${sampleArgs(verifyOp)}' --output json`,
    "```",
  ];
}

function noAuthSteps(strategyOps) {
  const verifyOp = strategyOps[0];
  return [
    "1. No external credential is required.",
    "2. Inspect the operation shape before use:",
    "",
    "```bash",
    `gum describe ${verifyOp.op_id}`,
    `${commandFor(verifyOp)} ${verifyOp.op_id} --args '${sampleArgs(verifyOp)}' --output json`,
    "```",
  ];
}

function unknownAuthSteps(strategy, strategyOps) {
  const verifyOp = strategyOps[0];
  return [
    `1. Inspect the operation. The catalog reports auth strategy \`${strategy}\`.`,
    "",
    "```bash",
    `gum describe ${verifyOp.op_id}`,
    "gum auth setup <op_id>",
    "```",
    "",
    "2. Complete the prerequisites reported by `gum auth setup` before dispatch.",
  ];
}

function authStrategyLabel(strategy) {
  if (strategy === "byo_oauth") return "Bring-your-own OAuth";
  if (strategy === "api_key") return "API key";
  if (strategy === "plugin_managed") return "Plugin-managed auth";
  if (strategy === "none") return "No external auth";
  return strategy;
}

function authGuideLabel(service) {
  return `${label(service)} auth guide`;
}

function uniqueScopes(serviceOps) {
  const scopes = new Set();
  for (const op of serviceOps) {
    for (const scope of firstVariant(op).scopes || []) scopes.add(scope);
  }
  return [...scopes].sort();
}

function shortScope(scope) {
  return String(scope).replace(/^https:\/\/www\.googleapis\.com\/auth\//, "");
}

function sortedServices() {
  return [...grouped.entries()].sort((a, b) => label(a[0]).localeCompare(label(b[0])));
}

function categoryFor(service) {
  return categories.get(service) || "Internal";
}

function label(service) {
  return labels.get(service) || service.replace(/(^|[-_])\w/g, (m) => m.toUpperCase()).replace(/[-_]/g, " ");
}

function slug(service) {
  return service.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
}

function firstVariant(op) {
  return op.variants?.[0] || {};
}

function commandFor(op) {
  const risk = firstVariant(op).risk_class;
  if (risk === "write") return "gum write";
  if (risk === "destructive") return "gum destructive";
  return "gum read";
}

function sampleArgs(op) {
  const fields = Array.isArray(op.request_fields) ? op.request_fields : [];
  const args = {};
  for (const field of fields.filter((item) => item.required).slice(0, 4)) {
    args[field.name] = sampleValue(field);
  }
  if (!Object.keys(args).length) args.fields = "id";
  return JSON.stringify(args).replaceAll("'", "\\'");
}

function sampleValue(field) {
  const name = String(field.name || "").toLowerCase();
  if (name.includes("userid")) return "me";
  if (name.includes("calendarid")) return "primary";
  if (name.includes("maxresults") || field.type === "integer") return 5;
  if (field.type === "boolean") return false;
  if (field.type === "array") return [];
  return `<${field.name}>`;
}

function searchQuery(service) {
  const serviceLabel = label(service).toLowerCase();
  if (service === "gmail") return "gmail messages";
  if (service === "drive") return "drive files";
  if (service === "calendar") return "calendar events";
  if (service === "sheets") return "sheets values";
  if (service === "docs") return "docs documents";
  return serviceLabel;
}

function riskSummary(serviceOps) {
  return mapSummary(countBy(serviceOps, (op) => firstVariant(op).risk_class || "unknown"));
}

function opCount(count) {
  return `${count} ${count === 1 ? "operation" : "operations"}`;
}

function countBy(items, keyFn) {
  const counts = new Map();
  for (const item of items) {
    const key = keyFn(item);
    counts.set(key, (counts.get(key) || 0) + 1);
  }
  return counts;
}

function mapSummary(counts) {
  return [...counts.entries()].sort((a, b) => a[0].localeCompare(b[0])).map(([key, count]) => `${count} ${key}`).join(", ");
}

function mdEscape(value) {
  return String(value || "").replace(/\|/g, "\\|").replace(/\r?\n/g, " ");
}
