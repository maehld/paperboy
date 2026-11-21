// Smoke-runs a browser script in jsdom, so an undefined identifier that
// `node --check` cannot see fails here. It loads the virtual display, answers
// one sync with a scrolling text page and waits for one marquee step.
import { readFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

let JSDOM;
try {
  ({ JSDOM } = createRequire(import.meta.url)("jsdom"));
} catch {
  console.error("jsdom is not installed; run `npm install --no-save jsdom` or set NODE_PATH.");
  process.exit(1);
}

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const script = await readFile(join(root, "server/internal/paperboy/ui/virtual.js"), "utf8");

const html = `<!doctype html><html><body data-sync="/ui/virtual/sync" data-reset="/ui/virtual/reset">
<img id="image" hidden><div id="text" data-cols="10" data-rows="2" hidden></div>
<p id="debug"></p><pre id="json"></pre>
<button id="reset" type="button">Reset</button><button id="full" type="button">Full screen</button>
<button data-role="previous" type="button">←</button><button data-role="home" type="button">Home</button><button data-role="next" type="button">→</button>
</body></html>`;

const answer = {
  text: {
    lines: ["ABCDEFGHIJ", "abcdefghij"],
    scrolls: [{
      row: 1,
      col: 0,
      width: 4,
      height: 1,
      text: ["0123456789"],
      window: ["0123"],
      step_ms: 50,
      direction: "horizontal",
    }],
    scroll_changed: true,
    hash: "h1",
  },
  refresh_s: 0,
  listen_s: 0,
  sleep: false,
  time: 1,
  debug: {},
};

const dom = new JSDOM(html, { url: "http://localhost/ui/virtual?plan=text&display=lcd", runScripts: "outside-only", pretendToBeVisual: true });
const { window } = dom;
const errors = [];
window.addEventListener("error", (event) => errors.push(event.error || event.message));
window.addEventListener("unhandledrejection", (event) => errors.push(event.reason));
window.fetch = async () => ({ ok: true, status: 200, json: async () => answer, text: async () => "" });

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const row = () => Array.from(window.document.querySelectorAll("#text .cell")).slice(10).map((cell) => cell.textContent).join("");

try {
  window.eval(script);
  await sleep(30);
  const debug = window.document.getElementById("debug").textContent;
  if (debug.includes("sync failed")) errors.push(debug);
  if (window.document.querySelectorAll("#text .cell").length !== 20) errors.push("the text grid has no cells");
  const first = row();
  await sleep(80);
  const next = row();
  if (first === next) errors.push("the scrolling box did not move: " + first);
  if (errors.length) {
    for (const error of errors) console.error(error);
    process.exitCode = 1;
  } else {
    console.log("virtual.js painted 20 cells and scrolled " + first + " → " + next);
  }
} finally {
  dom.window.close();
}
