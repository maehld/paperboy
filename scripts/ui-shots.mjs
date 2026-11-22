#!/usr/bin/env node
// Takes pictures of every operator page, so a change to the UI has a before
// and an after. The tool starts the screenshot instance of
// `scripts/screenshots` with `-serve`, drives a headless browser and writes one
// PNG per page and viewport.
//
//   npm install --no-save playwright
//   node scripts/ui-shots.mjs                  # into bin/ui-shots/
//   node scripts/ui-shots.mjs --only overview
//   node scripts/ui-shots.mjs --theme dark     # system dark mode
//   node scripts/ui-shots.mjs --bin bin/linux-amd64
//
// The instance is the example content, so the pictures need no credentials and
// no network. The pages are read-only; nothing here writes configuration.
import { spawn } from "node:child_process";
import { mkdir } from "node:fs/promises";
import { createRequire } from "node:module";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
let chromium;
try {
  ({ chromium } = require("playwright"));
} catch {
  console.error("playwright is not installed; run `npm install --no-save playwright` or set NODE_PATH.");
  process.exit(1);
}

const root = join(dirname(fileURLToPath(import.meta.url)), "..");

// Every page of the operator UI, with the example content it needs.
const pages = [
  { name: "overview", path: "/ui" },
  { name: "playground", path: "/ui/plugins" },
  { name: "configuration-pages", path: "/ui/pages" },
  { name: "configuration-plans", path: "/ui/plans" },
  { name: "configuration-devices", path: "/ui/devices" },
  { name: "configuration-displays", path: "/ui/display-types" },
  { name: "page-editor-image", path: "/ui/pages/uebersicht?display=e1001" },
  { name: "page-editor-text", path: "/ui/pages/hinweise?display=lcd2004" },
  { name: "plan-editor", path: "/ui/plans/standard" },
  { name: "device-editor", path: "/ui/devices/wohnzimmer" },
  { name: "display-editor", path: "/ui/display-types/sticky" },
  { name: "plan-preview", path: "/ui/devices/wohnzimmer/plan" },
  { name: "virtual-display", path: "/ui/virtual?plan=tour&display=sticky" },
  { name: "downloads", path: "/ui/system/downloads" },
  { name: "stats", path: "/ui/system/stats" },
  { name: "styleguide", path: "/ui/system/styleguide" },
];

// Two viewports: a laptop screen and a phone. The phone picture shows whether a
// table or a card row still works (issue #382).
const viewports = [
  { name: "desktop", width: 1440, height: 900 },
  { name: "phone", width: 390, height: 844 },
];

function usage(message) {
  if (message) console.error(message);
  console.error("usage: node scripts/ui-shots.mjs [--out DIR] [--only PAGE] [--theme light|dark] [--bin DIR]");
  process.exit(message ? 1 : 0);
}

const options = { out: join(root, "bin", "ui-shots"), only: "", theme: "light", bin: "" };
const argv = process.argv.slice(2);
for (let i = 0; i < argv.length; i++) {
  const arg = argv[i];
  const value = () => {
    if (i + 1 >= argv.length) usage(`${arg} needs a value`);
    return argv[++i];
  };
  if (arg === "--out") options.out = resolve(value());
  else if (arg === "--only") options.only = value();
  else if (arg === "--theme") options.theme = value();
  else if (arg === "--bin") options.bin = value();
  else if (arg === "--help" || arg === "-h") usage();
  else usage(`unknown option ${arg}`);
}
if (!["light", "dark"].includes(options.theme)) usage("--theme takes light or dark");

// The screenshot instance builds Paperboy and the plugins and serves the
// examples until stdin closes, so the browser talks to a real service.
function startInstance() {
  const args = ["./scripts/screenshots/main.go", "./scripts/screenshots/mock.go", "-serve"];
  if (options.bin) args.push("-bin", options.bin);
  const child = spawn("go", ["run", ...args], {
    cwd: root,
    stdio: ["pipe", "pipe", "inherit"],
    detached: true,
  });
  const url = new Promise((resolveURL, rejectURL) => {
    let buffer = "";
    const timer = setTimeout(() => rejectURL(new Error("the instance did not start in 5 minutes")), 5 * 60 * 1000);
    child.stdout.on("data", (chunk) => {
      buffer += chunk;
      let newline;
      while ((newline = buffer.indexOf("\n")) >= 0) {
        const line = buffer.slice(0, newline);
        buffer = buffer.slice(newline + 1);
        const match = /^paperboy=(https?:\/\/\S+)$/.exec(line);
        if (match) {
          clearTimeout(timer);
          resolveURL(match[1]);
        }
      }
    });
    child.on("exit", (code) => {
      clearTimeout(timer);
      rejectURL(new Error(`the instance stopped (exit ${code})`));
    });
  });
  return { child, url };
}

function stopInstance(child) {
  // The tool starts Paperboy as a child and kills it on exit, so the whole
  // process group has to go: `go run` wraps the server.
  child.stdin.end();
  try {
    process.kill(-child.pid, "SIGTERM");
  } catch {
    child.kill("SIGTERM");
  }
}

async function settle(page) {
  // Previews below the fold carry loading="lazy", so they load only when they
  // scroll into view. Walk the page first, then back to the top.
  await page.evaluate(async () => {
    const step = Math.max(200, Math.round(window.innerHeight * 0.8));
    for (let y = 0; y < document.documentElement.scrollHeight; y += step) {
      window.scrollTo(0, y);
      await new Promise((done) => requestAnimationFrame(() => setTimeout(done, 30)));
    }
    window.scrollTo(0, 0);
  });
  // Then wait for the pictures. A picture that never answers must not hang the
  // tool, so the wait has a ceiling.
  await page.evaluate(() =>
    Promise.race([
      Promise.all(
        Array.from(document.images).map((img) =>
          img.complete ? Promise.resolve() : new Promise((done) => {
            img.addEventListener("load", done, { once: true });
            img.addEventListener("error", done, { once: true });
          }),
        ),
      ),
      new Promise((done) => setTimeout(done, 5000)),
    ]),
  );
  await page.waitForTimeout(400);
}

async function main() {
  await mkdir(options.out, { recursive: true });
  const { child, url } = startInstance();
  let browser;
  try {
    const base = await url;
    console.log("instance:", base);
    browser = await chromium.launch();
    const shots = options.only ? pages.filter((p) => p.name === options.only) : pages;
    if (!shots.length) throw new Error(`no page named ${options.only}`);
    for (const viewport of viewports) {
      const context = await browser.newContext({
        viewport: { width: viewport.width, height: viewport.height },
        colorScheme: options.theme,
        reducedMotion: "reduce",
      });
      const page = await context.newPage();
      page.setDefaultTimeout(60_000);
      const problems = [];
      page.on("pageerror", (err) => problems.push(`${err.message}`));
      // A failed request names its URL, so a broken preview is easy to find.
      page.on("response", (res) => {
        if (res.status() >= 400) problems.push(`${res.status()} ${res.url()}`);
      });
      page.on("console", (msg) => {
        if (msg.type() === "error") problems.push(msg.text());
      });
      for (const shot of shots) {
        const target = join(options.out, `${shot.name}@${viewport.name}${options.theme === "dark" ? "-dark" : ""}.png`);
        try {
          const answer = await page.goto(base + shot.path, { waitUntil: "load", timeout: 60_000 });
          if (!answer || !answer.ok()) {
            console.error(`${shot.name}: HTTP ${answer ? answer.status() : "no answer"}`);
          }
          await settle(page);
          await page.screenshot({ path: target, fullPage: true, animations: "disabled", caret: "hide" });
          console.log("wrote", target);
        } catch (err) {
          // One page must not cost the other pictures.
          console.error(`${shot.name}@${viewport.name}: ${err.message}`);
        }
      }
      if (problems.length) {
        console.error(`browser errors on ${viewport.name}:`);
        for (const problem of new Set(problems)) console.error("  " + problem);
      }
      await context.close();
    }
  } finally {
    if (browser) await browser.close();
    stopInstance(child);
  }
}

main().catch((err) => {
  console.error("ui-shots:", err.message);
  process.exit(1);
});
