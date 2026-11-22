// Smoke-runs the browser scripts in jsdom, so an undefined identifier that
// `node --check` cannot see fails here. The first run loads the virtual
// display, answers one sync with a scrolling text page and waits for one
// marquee step. The dialog helper and the page editor follow below.
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

// dialog.js backs every prompt and confirmation of the editors (ADR 0150). jsdom
// has no modal dialog, so the check provides the two methods the helper calls
// and drives the promise instead of a click.
{
  const script = await readFile(join(root, "server/internal/paperboy/ui/dialog.js"), "utf8");
  const dialogDom = new JSDOM("<!doctype html><body></body>", { runScripts: "outside-only" });
  const { window } = dialogDom;
  window.HTMLDialogElement.prototype.showModal = function () {
    this.open = true;
  };
  window.HTMLDialogElement.prototype.close = function (value) {
    this.returnValue = value;
    this.dispatchEvent(new window.Event("close"));
  };
  window.eval(script);

  const problems = [];
  const check = (condition, message) => {
    if (!condition) problems.push(message);
  };

  const asked = window.pbAsk.text({ title: "Rename page", label: "New name", value: "alt" });
  const dialog = window.document.querySelector("dialog.ask");
  const input = dialog.querySelector("input");
  const ok = dialog.querySelector(".ask-ok");
  check(dialog.querySelector(".ask-title").textContent === "Rename page", "the dialog lost its title");
  check(input.value === "alt" && !ok.disabled, "the dialog did not start with its value");
  input.value = "";
  input.dispatchEvent(new window.Event("input"));
  check(ok.disabled, "the dialog accepts an empty name");
  input.value = "neu";
  input.dispatchEvent(new window.Event("input"));
  dialog.close("ok");
  check((await asked) === "neu", "the dialog lost the entered name");

  const cancelled = window.pbAsk.confirm({ title: "Delete?" });
  dialog.close("");
  check((await cancelled) === false, "a cancelled confirmation answered true");

  if (problems.length) {
    for (const problem of problems) console.error(problem);
    process.exitCode = 1;
  } else {
    console.log("dialog.js asked, validated and cancelled");
  }
  dialogDom.window.close();
}

// editor.js draws the widget palette of the Playground and of the page editor
// (ADR 0152). The check clicks an entry in both, because a palette that names
// its widgets wrongly still renders and only fails on the click (issue #391).
{
  const script = await readFile(join(root, "server/internal/paperboy/ui/editor.js"), "utf8");
  const plugins = {
    plugins: [{
      name: "demo",
      description: {
        title: "Demo",
        widgets: [
          { widget: "clock", title: "Clock", sizes: [{ columns: 2, rows: 1 }] },
          { widget: "chart", title: "Chart", sizes: [{ columns: 6, rows: 4 }] },
        ],
      },
    }],
  };
  const start = {
    name: "demo", display: "e1001", kind: "image", width: 800, height: 480,
    grid: [6, 8], reference: [6, 8], displays: [{ name: "e1001", kind: "image", width: 800, height: 480 }],
  };
  const palette = `<input id="palette-filter" type="search"><div id="palette"></div>`;
  const playground = `<div id="display-switch"><a data-display="e1001">e1001</a><span id="status"></span></div>
${palette}<span id="where"></span><img id="preview-img" hidden><pre id="preview-text" hidden></pre><p id="preview-error" hidden></p>
<section id="inspector"></section><button id="copy-cell" type="button"></button><pre id="snippet"></pre>`;
  const editor = `<h2><span id="file-label"></span></h2><span id="where"></span><span id="status"></span>
<button id="rename" type="button"></button><button id="delete" type="button"></button><button id="save" type="button"></button>
<input id="title"><p id="used-by"></p><input id="cols" type="number"><input id="rows" type="number">
<button id="grid-lock" type="button"></button><p id="grid-note"></p>
<input id="padding"><input id="rule"><input id="text-size"><input id="divider">
<select id="notices"><option value="">default</option><option value="none">none</option></select>
${palette}<span id="render-hint"></span><input id="show-cells" type="checkbox" checked>
<div id="board"></div><p id="board-note"></p><p id="preview-error" hidden></p><section id="inspector"></section>`;

  // open runs the script on one page and answers its requests.
  const open = async (url, body, data) => {
    const html = `<!doctype html><html><body>${body}
<script id="editor-data" type="application/json">${JSON.stringify(data)}</script></body></html>`;
    const editorDom = new JSDOM(html, { url, runScripts: "outside-only", pretendToBeVisual: true });
    const { window } = editorDom;
    const failures = [];
    window.addEventListener("error", (event) => failures.push(event.error || event.message));
    window.addEventListener("unhandledrejection", (event) => failures.push(event.reason));
    window.URL.createObjectURL = () => "blob:preview";
    window.URL.revokeObjectURL = () => {};
    window.fetch = async (target) => {
      const path = String(target);
      const answer = { ok: true, status: 200, headers: { get: () => "image/png" }, blob: async () => ({}), text: async () => "" };
      if (path.startsWith("/api/plugins")) return { ...answer, json: async () => plugins };
      if (path.startsWith("/api/pages/")) return { ...answer, status: 404, ok: false, json: async () => ({}) };
      return { ...answer, json: async () => ({}) };
    };
    window.eval(script);
    await sleep(30);
    return { editorDom, window, failures };
  };
  const entry = (window, name) => Array.from(window.document.querySelectorAll("#palette button.widget"))
    .find((button) => button.querySelector("strong").textContent === name);

  const problems = [];
  const check = (condition, message) => {
    if (!condition) problems.push(message);
  };

  // The Playground shows the widget a click names and keeps it in the link.
  {
    const { editorDom, window, failures } = await open("http://localhost/ui/playground?display=e1001", playground, { ...start, widgets_page: true });
    check(window.document.querySelectorAll("#palette button.widget").length === 2, "the Playground palette does not list the widgets");
    check(!window.document.querySelector("#palette button.widget.nofit"), "the Playground marks a widget as a misfit");
    entry(window, "chart").click();
    await sleep(10);
    check(new URLSearchParams(window.location.search).get("widget") === "demo/chart", "a click in the Playground names " + new URLSearchParams(window.location.search).get("widget"));
    check(window.document.getElementById("snippet").textContent.includes("widget: demo/chart"), "the snippet does not name the picked widget");
    check(window.localStorage.getItem("pb-recent-widgets") === '["demo/chart"]', "the recent list holds " + window.localStorage.getItem("pb-recent-widgets"));
    entry(window, "clock").querySelector(".pin").click();
    check(window.localStorage.getItem("pb-pinned-widgets") === '["demo/clock"]', "the pinned list holds " + window.localStorage.getItem("pb-pinned-widgets"));
    check(Array.from(window.document.querySelectorAll("#palette .plugin-head")).some((head) => head.textContent.includes("Pinned")), "a pinned widget gets no group");
    for (const failure of failures) problems.push(failure);
    editorDom.window.close();
  }

  // The page editor marks the picked entry and places it on the clicked cell.
  {
    const { editorDom, window, failures } = await open("http://localhost/ui/pages/demo?display=e1001", editor, { ...start, new: true });
    entry(window, "clock").click();
    check(entry(window, "clock").classList.contains("active"), "a click in the page editor does not mark the entry");
    const slot = window.document.querySelector("#board .slot");
    check(!!slot, "the board offers no free cell while a widget waits");
    if (slot) slot.click();
    check(Array.from(window.document.querySelectorAll("#board .cell")).some((cell) => cell.textContent.includes("demo/clock")), "the board does not show the placed widget");
    for (const failure of failures) problems.push(failure);
    editorDom.window.close();
  }

  if (problems.length) {
    for (const problem of problems) console.error(problem);
    process.exitCode = 1;
  } else {
    console.log("editor.js picked, pinned and placed a palette widget");
  }
}

// zoom.js opens the dashboard preview in the dialog outside the polled region
// (issue #398). jsdom has no modal dialog, so the check shims showModal/close.
{
  const script = await readFile(join(root, "server/internal/paperboy/ui/zoom.js"), "utf8");
  const html = `<!doctype html><html><body>
<button type="button" data-zoom data-kind="image" data-device="flur" data-page="uebersicht" data-page-url="/ui/pages/uebersicht" data-served="2 min ago"><img src="/image.png"></button>
<button type="button" data-zoom data-kind="text" data-device="lcd" data-page="hinweise"><pre>eins
zwei</pre></button>
<dialog id="zoom"><button data-close></button><figure><img><pre hidden></pre><figcaption><p><strong class="zoom-device"></strong> · <a class="zoom-page"></a></p><p class="zoom-served" hidden></p></figcaption></figure></dialog>
</body></html>`;
  const dom = new JSDOM(html, { url: "http://localhost/ui", runScripts: "outside-only" });
  const { window } = dom;
  const failures = [];
  window.addEventListener("error", (event) => failures.push(event.error || event.message));
  window.HTMLDialogElement.prototype.showModal = function () {
    this.open = true;
  };
  window.HTMLDialogElement.prototype.close = function () {
    this.open = false;
  };
  window.eval(script);

  const problems = [];
  const check = (condition, message) => {
    if (!condition) problems.push(message);
  };
  const buttons = window.document.querySelectorAll("button[data-zoom]");
  const dialog = window.document.getElementById("zoom");

  buttons[0].click();
  check(dialog.open, "the image preview did not open the dialog");
  check(dialog.querySelector("img").src.endsWith("/image.png"), "the dialog lost the preview source");
  check(!dialog.querySelector("img").hidden && dialog.querySelector("pre").hidden, "the dialog shows the wrong pane");
  check(dialog.querySelector(".zoom-device").textContent === "flur", "the dialog lost the device");
  check(dialog.querySelector(".zoom-page").getAttribute("href") === "/ui/pages/uebersicht", "the dialog lost the page link");
  check(dialog.querySelector(".zoom-served").textContent === "Served 2 min ago", "the dialog lost the served time");

  dialog.querySelector("[data-close]").click();
  check(!dialog.open, "the close button did not close the dialog");

  buttons[1].click();
  check(dialog.querySelector("pre").textContent === "eins\nzwei", "the dialog lost the text");
  check(dialog.querySelector("img").hidden && !dialog.querySelector("pre").hidden, "the text pane is hidden");
  check(!dialog.querySelector(".zoom-page").hasAttribute("href"), "a page without a URL kept a link");

  for (const failure of failures) problems.push(failure);
  if (problems.length) {
    for (const problem of problems) console.error(problem);
    process.exitCode = 1;
  } else {
    console.log("zoom.js opened the image and the text preview");
  }
  dom.window.close();
}
