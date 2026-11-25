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
        actions: [{
          name: "mode",
          title: "Charge mode",
          args: {
            index: { type: "int", default: 0, description: "loadpoint, counted from 0" },
            mode: { type: "string", required: true, enum: ["off", "pv"], enum_labels: { off: "Off", pv: "PV" } },
            target: { type: "int", required: true, description: "required target" },
          },
        }],
      },
    }, {
      name: "extra",
      description: {
        title: "Extra",
        widgets: [
          { widget: "gauge", title: "Gauge", sizes: [{ columns: 2, rows: 1 }] },
        ],
      },
    }],
  };
  const start = {
    name: "demo", display: "e1001", kind: "image", width: 800, height: 480, ppi: 125,
    grid: [6, 8], reference: [6, 8], displays: [{ name: "e1001", kind: "image", width: 800, height: 480 }],
  };
  const palette = `<input id="palette-filter" type="search"><div id="palette"></div>`;
  const playground = `<div id="display-switch"><a data-display="e1001">e1001</a><span id="status"></span></div>
${palette}<span id="preview-where"></span><img id="preview-img" hidden><pre id="preview-text" hidden></pre><p id="preview-error" hidden></p>
<section id="inspector"></section><button id="copy-cell" type="button"></button><pre id="snippet"></pre>`;
  const editor = `<h2><span id="file-label"></span></h2><span id="preview-where"></span><span id="status"></span>
<button id="rename" type="button"></button><button id="delete" type="button"></button><button id="save" type="button"></button>
<input id="title"><input id="cols" type="number"><input id="rows" type="number">
<button id="grid-lock" type="button"></button>
<div id="header-fields"><input id="header-height" type="number"><button id="header-add" type="button"></button><button id="header-remove" type="button"></button></div>
<input id="padding"><input id="rule"><input id="text-size"><input id="divider">
<select id="notices"><option value="">default</option><option value="none">none</option></select>
${palette}<span id="render-hint"></span><input id="show-cells" type="checkbox" checked>
<div id="board"></div><p id="board-note"></p><p id="preview-error" hidden></p><section id="inspector"></section>`;

  // open runs the script on one page and answers its requests.
  const open = async (url, body, data, routes = {}) => {
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
      if (path.startsWith("/api/pages/")) {
        if (routes.page) return { ...answer, json: async () => routes.page };
        return { ...answer, status: 404, ok: false, json: async () => ({}) };
      }
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
  // section and entriesOf read one palette group by its title, which is the
  // head's only span without a class (the chevron and the count carry one).
  const section = (window, name) => Array.from(window.document.querySelectorAll("#palette .plugin"))
    .find((group) => {
      const label = Array.from(group.querySelector(".plugin-head").children).find((child) => !child.className);
      return label && label.textContent.startsWith(name);
    });
  const entriesOf = (window, name) => {
    const group = section(window, name);
    return group ? Array.from(group.querySelectorAll("button.widget")) : [];
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
    entry(window, "clock").querySelector(".pin").click();
    check(window.localStorage.getItem("pb-pinned-widgets") === '["demo/clock"]', "the pinned list holds " + window.localStorage.getItem("pb-pinned-widgets"));
    check(Array.from(window.document.querySelectorAll("#palette .plugin-head")).some((head) => head.textContent.includes("Pinned")), "a pinned widget gets no group");
    // One plugin group is open: the head of another swaps it, and a filter
    // shows the matches of every group.
    check(entriesOf(window, "demo").length === 2, "the palette opens more than one plugin group");
    check(entriesOf(window, "extra").length === 0, "a folded plugin group still lists its widgets");
    const extraHead = section(window, "extra").querySelector(".plugin-head");
    extraHead.click();
    check(entriesOf(window, "demo").length === 0 && entriesOf(window, "extra").length === 1, "a head click does not swap the open plugin group");
    check(section(window, "extra").querySelector(".plugin-head").getAttribute("aria-expanded") === "true", "the open head does not say aria-expanded");
    const filter = window.document.getElementById("palette-filter");
    filter.value = "gauge";
    filter.dispatchEvent(new window.Event("input"));
    check(!section(window, "demo"), "a filter lists a group without a match");
    check(entriesOf(window, "extra").length === 1, "a filter hides the match of a folded group");
    check(!!section(window, "extra").querySelector(".plugin-head.static"), "a filtered head still claims to fold");
    filter.value = "";
    filter.dispatchEvent(new window.Event("input"));
    check(entriesOf(window, "extra").length === 1 && entriesOf(window, "demo").length === 0, "clearing the filter does not return to the open group");
    for (const failure of failures) problems.push(failure);
    editorDom.window.close();
  }

  // The page editor marks the picked entry and places it on the clicked cell.
  {
    const { editorDom, window, failures } = await open("http://localhost/ui/pages/demo?display=e1001", editor, { ...start, new: true });
    check(window.document.getElementById("board-note").textContent.includes("px on e1001"), "the board note does not name the cell size");
    entry(window, "clock").click();
    check(entry(window, "clock").classList.contains("active"), "a click in the page editor does not mark the entry");
    const slot = window.document.querySelector("#board .slot");
    check(!!slot, "the board offers no free cell while a widget waits");
    if (slot) slot.click();
    check(Array.from(window.document.querySelectorAll("#board .cell")).some((cell) => cell.textContent.includes("demo/clock")), "the board does not show the placed widget");
    // A header band starts with the built-in title; a palette click on the
    // band adds a part to the zone under the pointer (ADR 0166).
    window.document.getElementById("header-add").click();
    await sleep(10);
    const band = window.document.getElementById("board-header");
    check(!!band && band.querySelectorAll(".header-part").length === 1, "the board does not show the added header band");
    check(window.document.getElementById("header-height").value === "24", "the header band does not start at 24 pt");
    check(window.document.getElementById("inspector").textContent.includes("Header part"), "the inspector does not edit the header part");
    if (band) {
      band.getBoundingClientRect = () => ({ left: 0, top: 0, width: 800, height: 40, right: 800, bottom: 40 });
      const demoHead = section(window, "demo").querySelector(".plugin-head");
      if (demoHead) demoHead.click();
      entry(window, "clock").click();
      const liveBand = window.document.getElementById("board-header");
      if (liveBand) {
        liveBand.getBoundingClientRect = () => ({ left: 0, top: 0, width: 800, height: 40, right: 800, bottom: 40 });
        liveBand.dispatchEvent(new window.MouseEvent("click", { bubbles: true, clientX: 700, clientY: 10 }));
      }
      await sleep(10);
      const afterBand = window.document.getElementById("board-header");
      check(!!afterBand && afterBand.querySelectorAll(".header-part").length === 2, "a click on the header band adds no part");
    }
    for (const failure of failures) problems.push(failure);
    editorDom.window.close();
  }

  // The inspector binds one action with its own arguments, not the widget's
  // (ADR 0164). An empty required enum is the choice; a missing one disables
  // the action on the device.
  {
    const page = { file: "", hash: "h", template: { kind: "image", grid: [6, 8], style: {}, cells: [
      { at: [0, 0], span: [2, 1], widget: "demo/clock", args: {}, action: { name: "mode", args: {} } },
    ] } };
    const { editorDom, window, failures } = await open("http://localhost/ui/pages/demo?display=e1001", editor, start, { page });
    const board = window.document.getElementById("board");
    const rect = () => ({ left: 0, top: 0, width: 800, height: 480, right: 800, bottom: 480 });
    board.getBoundingClientRect = rect;
    const grid = window.document.getElementById("board-grid");
    if (grid) grid.getBoundingClientRect = rect;
    const cell = window.document.querySelector("#board .cell");
    check(!!cell, "the board did not draw the actionable cell");
    if (cell) {
      cell.dispatchEvent(new window.MouseEvent("pointerdown", { bubbles: true, button: 0, clientX: 10, clientY: 10 }));
      window.dispatchEvent(new window.MouseEvent("pointerup", { bubbles: true, button: 0, clientX: 10, clientY: 10 }));
    }
    const inspector = window.document.getElementById("inspector");
    const fieldOf = (name) => Array.from(inspector.querySelectorAll("label.field"))
      .find((label) => label.querySelector("span").textContent === name);
    const argOf = (name) => Array.from(inspector.querySelectorAll(".arg"))
      .find((arg) => arg.querySelector(".name").textContent.replace(/\s*\*$/, "") === name);
    const action = fieldOf("Action");
    check(action && action.querySelector("select").value === "mode", "the inspector lost the cell action");
    const index = argOf("index");
    const indexInput = index && index.querySelector("input");
    check(indexInput && indexInput.value === "" && indexInput.placeholder === "0", "the action does not show the default index as a placeholder");
    const mode = argOf("mode");
    const select = mode && mode.querySelector("select");
    check(select && select.value === "" && select.options[0].textContent.includes("device offers"), "an empty required enum is not the choice");
    check(inspector.textContent.includes("Empty: the device offers the values."), "the action lacks the choice hint");
    // A missing required argument warns live.
    check(inspector.textContent.includes("Required argument target is missing"), "a missing action argument does not warn");
    const target = argOf("target").querySelector("input");
    target.value = "0";
    target.dispatchEvent(new window.Event("input"));
    target.dispatchEvent(new window.Event("change"));
    check(inspector.textContent.includes("A short home shows 2 options here."), "the action hint does not count the choices");
    // A set enum is one fixed tile; the hint follows without a reload.
    const fixed = argOf("mode").querySelector("select");
    fixed.value = "pv";
    fixed.dispatchEvent(new window.Event("change"));
    check(inspector.textContent.includes("A short home shows this action here."), "a fixed action still counts choices");
    for (const failure of failures) problems.push(failure);
    editorDom.window.close();
  }

  // A variant shows the title of its page file, read-only (ADR 0110). The
  // title used to stay empty because the script never read page_title.
  {
    const page = { file: "lcd", hash: "h", template: { kind: "image", grid: [6, 8], style: {}, cells: [] } };
    const { editorDom, window, failures } = await open("http://localhost/ui/pages/demo?display=e1001&file=lcd", editor,
      { ...start, file: "lcd", page_title: "Solar" }, { page });
    const title = window.document.getElementById("title");
    check(title.value === "Solar", "a variant does not show the title of its page file: " + JSON.stringify(title.value));
    check(title.readOnly, "a variant lets its inherited title be edited");
    check(title.title.includes("pages/demo.yaml"), "the inherited title does not name its file: " + JSON.stringify(title.title));
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

// titlebar.js keeps one primary action and moves the rest into the overflow
// menu on a narrow screen (issue #397). Moving nodes keeps their listeners.
{
  const script = await readFile(join(root, "server/internal/paperboy/ui/titlebar.js"), "utf8");
  const html = `<!doctype html><html><body>
<div class="topbar-end"><div class="title-actions">
<button id="rename" type="button" hidden>Rename</button>
<button id="delete" type="button" class="danger">Delete</button>
<button id="reload" type="button">Reload</button>
<button id="save" type="button" class="primary">Save</button>
</div><div class="action-overflow"><button id="action-more" type="button" aria-expanded="false" hidden>⋯</button><div id="action-menu" role="menu" hidden></div></div></div>
</body></html>`;
  const dom = new JSDOM(html, { url: "http://localhost/ui", runScripts: "outside-only" });
  const { window } = dom;
  const listeners = [];
  const media = {
    matches: true,
    addEventListener: (type, fn) => type === "change" && listeners.push(fn),
  };
  window.matchMedia = () => media;
  window.eval(script);

  const problems = [];
  const check = (condition, message) => {
    if (!condition) problems.push(message);
  };
  const actions = window.document.querySelector(".title-actions");
  const menu = window.document.getElementById("action-menu");
  const more = window.document.getElementById("action-more");
  const byId = (id) => window.document.getElementById(id);

  check(!more.hidden, "the narrow title bar has no overflow button");
  check(actions.contains(byId("save")), "the primary action left the title bar");
  check(menu.contains(byId("rename")) && menu.contains(byId("delete")) && menu.contains(byId("reload")), "the secondary actions are not in the menu");
  check(!actions.contains(byId("reload")), "a secondary action stayed in the title bar");

  more.click();
  check(!menu.hidden && more.getAttribute("aria-expanded") === "true", "the overflow menu did not open");
  menu.click();
  check(menu.hidden && more.getAttribute("aria-expanded") === "false", "an action click did not close the menu");

  media.matches = false;
  for (const fn of listeners) fn();
  check(more.hidden, "the wide title bar still shows the overflow button");
  check(["rename", "delete", "reload", "save"].every((id) => actions.contains(byId(id))), "the wide title bar lost an action");
  check(byId("save").nextElementSibling === null, "the wide title bar changed the action order");

  if (problems.length) {
    for (const problem of problems) console.error(problem);
    process.exitCode = 1;
  } else {
    console.log("titlebar.js collapsed and restored the actions");
  }
  dom.window.close();
}
