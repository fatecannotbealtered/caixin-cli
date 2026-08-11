#!/usr/bin/env node
// live-smoke.js — run every declared read command against the real service and
// check that what comes back matches what `reference` promises.
//
// This is the layer the mock tests cannot reach. Mock upstream answers with
// shapes this repo wrote itself, so it proves the envelope, the error mapping,
// and the exit codes -- but it can only ever confirm our own assumptions. It
// could not, and did not, catch that nobody could sign in, or that the signed
// full-text endpoint answered 401 because the login cookies are scoped to a
// different host than it lives on.
//
// It is deliberately NOT in CI. There is no sandbox and no test account tier;
// the only environment is the live service read with a real signed-in account
// (docs/E2E.md). Run it by hand before a release, and keep the report.
//
// Read-only throughout: every Caixin command is a read, and the three that
// change local state or the binary are skipped by name.
//
// Every url this runs against is harvested from a listing. Caixin's commands
// require their target to be currently published by a parent directory, so an
// invented url would test the refusal path and be reported as coverage.
//
// Usage:
//   node scripts/live-smoke.js
//   node scripts/live-smoke.js --bin ./caixin-cli
//   node scripts/live-smoke.js --report out.json
//   node scripts/live-smoke.js --json
//
// Exit codes:
//   0  every command that ran matched its declared schema
//   1  at least one command returned a field it does not declare, or failed
//   2  the tool could not be run, or no session is loaded

"use strict";
const fs = require("fs");
const path = require("path");
const { spawnSync } = require("child_process");

const TOOL = "caixin-cli";

const SKIP = {
  login: "starts a human-in-the-loop QR handshake",
  "login-resume": "settles a human-in-the-loop QR handshake",
  logout: "deletes local credentials",
  update: "replaces the running binary",
};

function arg(name, fallback) {
  const index = process.argv.indexOf(name);
  return index >= 0 && process.argv[index + 1] ? process.argv[index + 1] : fallback;
}
const flag = (name) => process.argv.includes(name);

const bin = arg("--bin", TOOL);
const reportPath = arg("--report", "live-smoke-report.json");
const asJSON = flag("--json");

function run(args) {
  const result = spawnSync(bin, [...args, "--compact"], {
    encoding: "utf8",
    maxBuffer: 64 * 1024 * 1024,
  });
  if (result.error) return { spawnFailed: String(result.error.message) };
  try {
    return { exit: result.status, envelope: JSON.parse(result.stdout) };
  } catch {
    return { exit: result.status, parseFailed: true };
  }
}

// ---- discovery -------------------------------------------------------------

const reference = run(["reference"]);
if (reference.spawnFailed || !reference.envelope || !reference.envelope.ok) {
  console.error(`live-smoke: could not read \`${bin} reference\`.`);
  process.exit(2);
}
const ref = reference.envelope.data;
const schemas = ref.schemas || {};

const leaves = [];
(function walk(commands, prefix) {
  for (const command of commands || []) {
    const name = (prefix ? `${prefix} ` : "") + command.name;
    if (command.children && command.children.length) walk(command.children, name);
    else
      leaves.push({
        name,
        schema: command.output_schema,
        params: command.params || [],
        examples: command.examples || [],
      });
  }
})(ref.commands, "");

const status = run(["status"]);
if (!status.envelope || !status.envelope.ok || !status.envelope.data.authenticated) {
  console.error("live-smoke: no Caixin session. Run `caixin-cli login` first.");
  process.exit(2);
}

// ---- url harvest -----------------------------------------------------------
//
// Every url comes out of a live listing. `collectURLs` walks a payload rather
// than assuming a shape, because the listing commands differ: some answer with
// `articles`, some with `modules[].items`, some with a directory envelope.

function collectURLs(value, found = []) {
  if (Array.isArray(value)) {
    for (const item of value) collectURLs(item, found);
  } else if (value && typeof value === "object") {
    for (const [key, nested] of Object.entries(value)) {
      if (key === "url" && typeof nested === "string" && nested.startsWith("http")) {
        found.push(nested);
      } else {
        collectURLs(nested, found);
      }
    }
  }
  return found;
}

function urlsFrom(...args) {
  const outcome = run(args);
  if (!outcome.envelope || !outcome.envelope.ok) return [];
  return collectURLs(outcome.envelope.data);
}

const pool = [
  ...urlsFrom("latest"),
  ...urlsFrom("newscroll"),
  ...urlsFrom("snapshot", "https://www.caixin.com/"),
  ...urlsFrom("snapshot", "https://opinion.caixin.com/"),
  ...urlsFrom("snapshot", "https://culture.caixin.com/"),
  ...urlsFrom("public-directory", "https://datanews.caixin.com/datatopic/"),
  ...urlsFrom("bloggers-directory"),
  // The directory publishes both article permalinks and author roots; the root
  // is what `blog-author` reads, so it is added explicitly.
  ...urlsFrom("bloggers-directory")
    .filter((url) => /\.blog\.caixin\.com/.test(url))
    .map((url) => url.replace(/^(https:\/\/[^/]+).*$/, "$1")),
];

// `route` is the tool's own answer to "which command reads this url", and it
// returns the exact argv to use. Asking it beats pattern-matching hostnames
// here: the allowlists live in the tool, and a url this script classified by
// hand would drift from them silently.
//
// One url per shape is routed, and the sample is capped: every route call is a
// live request, and the point is one usable url per adapter, not a survey.
const shapes = new Map();
for (const url of new Set(pool)) {
  try {
    const parsed = new URL(url);
    const key = `${parsed.hostname}|${parsed.pathname.split("/")[1] || ""}|${/\d{6,}\.html$/.test(url)}`;
    if (!shapes.has(key)) shapes.set(key, url);
  } catch {
    // Not a url this script can classify; the pool is best-effort.
  }
}

// Round-robin across hosts, because one host can have hundreds of shapes and a
// straight walk spends the whole budget inside it before reaching the adapters
// that only appear elsewhere.
const byHost = new Map();
for (const url of shapes.values()) {
  const host = new URL(url).hostname;
  if (!byHost.has(host)) byHost.set(host, []);
  byHost.get(host).push(url);
}
const ordered = [];
for (let depth = 0; ordered.length < shapes.size; depth += 1) {
  let added = false;
  for (const list of byHost.values()) {
    if (depth < list.length) {
      ordered.push(list[depth]);
      added = true;
    }
  }
  if (!added) break;
}

const routed = new Map();
let routeCalls = 0;
for (const url of ordered) {
  if (routeCalls >= 90) break;
  routeCalls += 1;
  const verdict = run(["route", url]);
  if (!verdict.envelope || !verdict.envelope.ok) continue;
  const data = verdict.envelope.data;
  if (!data.supported || !Array.isArray(data.command) || !data.command.length) continue;
  const [adapter, ...args] = data.command;
  if (!routed.has(adapter)) routed.set(adapter, args);
}

const frontline = run(["frontline"]);
let frontlineCode = null;
if (frontline.envelope && frontline.envelope.ok) {
  const coded = JSON.stringify(frontline.envelope.data).match(/"code":"([0-9a-f]{16,})"/);
  if (coded) frontlineCode = coded[1];
}
const routedArticle = routed.get("article");

// ---- argument construction -------------------------------------------------

function argsFor(leaf) {
  // Commands whose argument is not a url, or where a specific one is wanted.
  const fixed = {
    snapshot: ["https://www.caixin.com/"],
    "public-directory": ["https://datanews.caixin.com/datatopic/"],
    "cxdata-feed": ["frontline"],
    "entities-preview": ["companies"],
    search: ["经济"],
    "frontline-detail": frontlineCode && [frontlineCode],
    // `route` classifies a url; any harvested one exercises it.
    route: routedArticle && [routedArticle[0]],
    // The signed full body is the capability this tool exists for, so the
    // article read is always the full one.
    article: routedArticle && [...routedArticle, "--full"],
  };
  if (Object.prototype.hasOwnProperty.call(fixed, leaf.name)) return fixed[leaf.name] || null;
  // `blog-author` is the exception: its discovery reads only the front of the
  // blog index, while every listing this script harvests from is the far larger
  // roster. A harvested author is usually refused, so the declared example --
  // which is on the front page -- is used instead.
  if (leaf.name !== "blog-author" && routed.has(leaf.name)) return routed.get(leaf.name);
  // `reference` declares a runnable example per command, and its entry url is
  // one the allowlist accepts. Falling back to it beats leaving the command
  // uncovered, and beats inventing a url here: if the example has gone stale
  // the command answers E_NOT_FOUND, which is recorded as the answer it is.
  const example = exampleArgs(leaf);
  if (example) return example;
  return leaf.params.filter((param) => param.required).length ? null : [];
}

function exampleArgs(leaf) {
  const example = (leaf.examples || [])[0];
  if (!example) return null;
  const tokens = example.split(/\s+/).filter((token) => token && token !== "--compact");
  // Drop the tool name and the command path itself.
  const words = leaf.name.split(" ").length;
  const args = tokens.slice(1 + words);
  return args.length ? args : null;
}

// ---- the run ---------------------------------------------------------------

const results = [];
for (const leaf of leaves) {
  if (SKIP[leaf.name]) {
    results.push({ command: leaf.name, status: "skipped", reason: SKIP[leaf.name] });
    continue;
  }
  const args = argsFor(leaf);
  if (!args) {
    results.push({
      command: leaf.name,
      status: "not_covered",
      reason: "no url for its required arguments could be harvested from a live listing",
    });
    continue;
  }

  let outcome = run([...leaf.name.split(" "), ...args]);
  // A burst of live reads gets throttled, and a throttled read is not a defect.
  // One retry separates "the service pushed back" from "this command is broken".
  const transient = (result) =>
    result.envelope && !result.envelope.ok &&
    ["E_TIMEOUT", "E_NETWORK", "E_RATE_LIMITED"].includes(result.envelope.error.code);
  if (transient(outcome)) {
    spawnSync(process.execPath, ["-e", "setTimeout(()=>{}, 4000)"]);
    outcome = run([...leaf.name.split(" "), ...args]);
  }
  if (outcome.spawnFailed || outcome.parseFailed) {
    results.push({ command: leaf.name, status: "failed", reason: "no JSON envelope" });
    continue;
  }
  const envelope = outcome.envelope;
  if (!envelope.ok) {
    // "Not published right now" and "the subscription does not cover this" are
    // answers, not faults; reporting them is what this tool is for.
    const code = (envelope.error && envelope.error.code) || "";
    const answered = ["E_NOT_FOUND", "E_FORBIDDEN", "E_VALIDATION", "E_RATE_LIMITED"].includes(code);
    results.push({ command: leaf.name, status: answered ? "answered" : "error", error_code: code });
    continue;
  }

  const declared = new Set((schemas[leaf.schema] || {}).fields || []);
  const actual = Object.keys(envelope.data || {}).filter((key) => key !== "_untrusted");
  const undeclared = actual.filter((key) => !declared.has(key));
  results.push({
    command: leaf.name,
    status: undeclared.length ? "undeclared_fields" : "match",
    schema: leaf.schema,
    undeclared_fields: undeclared,
    declared_but_absent: [...declared].filter((key) => !actual.includes(key)),
  });
}

// The one assertion that is about content rather than shape: a signed full-text
// read of a paywalled article must come back complete. It is the capability
// this tool exists for, and it answered 401 for its whole life until the
// session was serialised onto the request.
if (routedArticle) {
  const full = run(["article", ...routedArticle, "--full"]);
  const complete = Boolean(full.envelope && full.envelope.ok && full.envelope.data.complete);
  results.push({
    command: "article --full (completeness)",
    status: complete ? "match" : "error",
    error_code: complete ? undefined : "body was not complete",
  });
}

const counts = results.reduce((tally, entry) => {
  tally[entry.status] = (tally[entry.status] || 0) + 1;
  return tally;
}, {});
const failed = results.filter((entry) =>
  ["undeclared_fields", "failed", "error"].includes(entry.status)
);

fs.writeFileSync(
  reportPath,
  `${JSON.stringify(
    { tool: ref.tool, version: ref.version, ran_at: new Date().toISOString(), counts, results },
    null,
    2
  )}\n`
);

if (asJSON) {
  console.log(JSON.stringify({ counts, failures: failed.map((f) => f.command) }, null, 2));
} else {
  for (const entry of results) {
    const label = entry.command.padEnd(30);
    if (entry.status === "match") console.log(`  ok         ${label}`);
    else if (entry.status === "undeclared_fields")
      console.log(`  UNDECLARED ${label} ${entry.undeclared_fields.join(", ")}`);
    else if (entry.status === "answered") console.log(`  answered   ${label} ${entry.error_code}`);
    else if (entry.status === "error") console.log(`  ERROR      ${label} ${entry.error_code}`);
    else console.log(`  ${entry.status.padEnd(10)} ${label} ${entry.reason}`);
  }
  console.log();
  console.log(
    `live-smoke: ${counts.match || 0} matched, ${counts.undeclared_fields || 0} undeclared, ` +
      `${counts.answered || 0} answered-with-a-code, ${counts.error || 0} errored, ` +
      `${counts.not_covered || 0} not covered, ${counts.skipped || 0} skipped`
  );
  console.log(`report: ${reportPath}`);
}

process.exit(failed.length ? 1 : 0);
