// The command palette of the operator UI (ADR 0152). Cmd+K or Ctrl+K opens a
// search over the sections and the objects of the instance and jumps to one.
// The lists come from the APIs the pages already use, so the server needs no
// search route and the palette knows the same names as the rail and the lists.
(() => {
  "use strict";

  const isMac = /Mac|iPhone|iPad/.test(navigator.platform || "");
  const key = isMac ? "⌘K" : "Ctrl+K";

  // sections are the entries that need no request.
  const sections = [
    { label: "Devices", hint: "the fleet", url: "/ui", group: "Go to" },
    { label: "Pages", hint: "page files and variants", url: "/ui/pages", group: "Go to" },
    { label: "Plans", hint: "pages per device", url: "/ui/plans", group: "Go to" },
    { label: "Playground", hint: "one widget in its cell", url: "/ui/playground", group: "Go to" },
    { label: "Plugins", hint: "installed programs and the release check", url: "/ui/system/plugins", group: "Go to" },
    { label: "Firmware", hint: "installed images and the release check", url: "/ui/system/firmware", group: "Go to" },
    { label: "Maintenance", hint: "clean up what Paperboy no longer needs", url: "/ui/system/maintenance", group: "Go to" },
    { label: "Stats", hint: "runtime, plugins, caches", url: "/ui/system/stats", group: "Go to" },
    { label: "Styleguide", hint: "tokens and components", url: "/ui/system/styleguide", group: "Go to" },
  ];

  let entries = null; // filled on the first open
  let loading = null;

  async function load() {
    const get = async (path) => {
      const res = await fetch(path, { headers: { Accept: "application/json" } });
      if (!res.ok) throw new Error(path + ": " + res.status);
      return res.json();
    };
    const [pages, plans, devices, displays, plugins] = await Promise.all([
      get("/api/pages"),
      get("/api/plans"),
      get("/api/devices"),
      get("/api/displays"),
      get("/api/plugins"),
    ]);
    const list = [];
    const arrayOf = (answer, field) => (Array.isArray(answer) ? answer : (answer && answer[field]) || []);

    for (const page of arrayOf(pages, "pages")) {
      list.push({
        label: page.name,
        hint: [page.title, page.kind].filter(Boolean).join(" · "),
        group: "Pages",
        url: "/ui/pages/" + encodeURIComponent(page.name),
      });
    }
    for (const plan of arrayOf(plans, "plans")) {
      list.push({
        label: plan.name,
        hint: (plan.pages || []).length + " pages · " + (plan.triggers || 0) + " triggers",
        group: "Plans",
        url: "/ui/plans/" + encodeURIComponent(plan.name),
      });
    }
    for (const device of arrayOf(devices, "devices")) {
      list.push({
        label: device.id,
        hint: [device.display_type, device.plan].filter(Boolean).join(" · "),
        group: "Devices",
        url: "/ui/devices/" + encodeURIComponent(device.id),
      });
      if (device.plan) {
        list.push({
          label: device.id + " plan",
          hint: "the plan preview of " + device.id,
          group: "Devices",
          url: "/ui/devices/" + encodeURIComponent(device.id) + "/plan",
        });
      }
    }
    for (const display of arrayOf(displays, "displays")) {
      list.push({
        label: display.name,
        hint: [display.kind, display.size ? display.size.join("×") : ""].filter(Boolean).join(" · "),
        group: "Display types",
        url: "/ui/display-types/" + encodeURIComponent(display.name),
      });
    }
    // A widget opens in the Playground, so the entry needs an image display.
    const imageType = (arrayOf(displays, "displays").find((d) => d.kind === "image") || {}).name;
    if (imageType) {
      for (const plugin of arrayOf(plugins, "plugins")) {
        for (const widget of ((plugin.description && plugin.description.widgets) || [])) {
          if (widget.kinds && widget.kinds.length && !widget.kinds.includes("image")) continue;
          list.push({
            label: widget.widget,
            hint: plugin.name + (widget.title ? " · " + widget.title : ""),
            group: "Widgets",
            keywords: plugin.name + " " + widget.widget + " " + (widget.title || ""),
            url: "/ui/playground?" + new URLSearchParams({
              widget: plugin.name + "/" + widget.widget,
              display: imageType,
            }).toString(),
          });
        }
      }
    }
    return list;
  }

  function prepare() {
    if (entries) return Promise.resolve(entries);
    if (!loading) {
      loading = load()
        .then((list) => {
          entries = list;
          return list;
        })
        .catch(() => {
          entries = [];
          return entries;
        });
    }
    return loading;
  }

  // score ranks a match: 0 starts the label, 1 starts a word, 2 is inside it,
  // 3 only matches the keywords. Lower is better.
  function score(entry, query) {
    const label = entry.label.toLowerCase();
    if (label === query) return 0;
    if (label.startsWith(query)) return 1;
    if (new RegExp("\\b" + query.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).test(label)) return 2;
    if (label.includes(query)) return 3;
    const haystack = (entry.keywords || "") + " " + entry.group + " " + (entry.hint || "");
    if (haystack.toLowerCase().includes(query)) return 4;
    return -1;
  }

  function search(all, raw) {
    const query = raw.trim().toLowerCase();
    if (!query) {
      // Without a query the palette offers the sections and a few objects.
      return all.slice(0, 12);
    }
    return all
      .map((entry) => ({ entry, rank: score(entry, query) }))
      .filter((item) => item.rank >= 0)
      .sort((a, b) => a.rank - b.rank || a.entry.label.localeCompare(b.entry.label))
      .slice(0, 30)
      .map((item) => item.entry);
  }

  let dialog = null;
  let input = null;
  let list = null;
  let shown = [];
  let active = 0;

  function build() {
    dialog = document.createElement("dialog");
    dialog.className = "cmd";
    dialog.innerHTML = `
      <form method="dialog" class="cmd-form">
        <input class="cmd-input" type="search" autocomplete="off" spellcheck="false"
               placeholder="Search pages, plans, devices, widgets…" aria-label="Search">
        <ul class="cmd-results" role="listbox" aria-label="Results"></ul>
        <p class="cmd-foot"><span class="cmd-hint"></span><span class="cmd-keys"><kbd>↑</kbd><kbd>↓</kbd> choose · <kbd>Enter</kbd> open · <kbd>Esc</kbd> close</span></p>
      </form>`;
    document.body.appendChild(dialog);
    input = dialog.querySelector(".cmd-input");
    list = dialog.querySelector(".cmd-results");
    input.addEventListener("input", () => render());
    input.addEventListener("keydown", (e) => {
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        move(e.key === "ArrowDown" ? 1 : -1);
      } else if (e.key === "Enter") {
        e.preventDefault();
        open(active);
      }
    });
    list.addEventListener("click", (e) => {
      const item = e.target.closest("[data-index]");
      if (item) open(Number(item.dataset.index));
    });
    list.addEventListener("mousemove", (e) => {
      const item = e.target.closest("[data-index]");
      if (item && Number(item.dataset.index) !== active) {
        active = Number(item.dataset.index);
        mark();
      }
    });
  }

  function mark() {
    for (const item of list.querySelectorAll("[data-index]")) {
      const on = Number(item.dataset.index) === active;
      item.classList.toggle("active", on);
      item.setAttribute("aria-selected", on ? "true" : "false");
      if (on) item.scrollIntoView({ block: "nearest" });
    }
  }

  function move(step) {
    if (!shown.length) return;
    active = (active + step + shown.length) % shown.length;
    mark();
  }

  function open(index) {
    const entry = shown[index];
    if (!entry) return;
    dialog.close();
    window.location.assign(entry.url);
  }

  function render() {
    const all = entries || [];
    const query = input.value;
    const pool = query.trim() ? all : sections.concat(all);
    shown = search(pool, query);
    active = 0;
    list.innerHTML = "";
    if (!shown.length) {
      const empty = document.createElement("li");
      empty.className = "cmd-empty";
      empty.textContent = all.length ? "Nothing matches “" + query.trim() + "”." : "The lists are not available.";
      list.appendChild(empty);
      dialog.querySelector(".cmd-hint").textContent = "";
      return;
    }
    let group = "";
    for (const [index, entry] of shown.entries()) {
      if (entry.group !== group) {
        group = entry.group;
        const head = document.createElement("li");
        head.className = "cmd-group";
        head.setAttribute("role", "presentation");
        head.textContent = group;
        list.appendChild(head);
      }
      const item = document.createElement("li");
      item.className = "cmd-item";
      item.dataset.index = String(index);
      item.setAttribute("role", "option");
      const label = document.createElement("span");
      label.className = "cmd-label";
      label.textContent = entry.label;
      const hint = document.createElement("span");
      hint.className = "cmd-note";
      hint.textContent = entry.hint || "";
      item.append(label, hint);
      list.appendChild(item);
    }
    mark();
    dialog.querySelector(".cmd-hint").textContent = "";
  }

  async function show() {
    if (!dialog) build();
    if (dialog.open) return;
    dialog.showModal();
    input.value = "";
    list.innerHTML = "";
    dialog.querySelector(".cmd-hint").textContent = "Loading…";
    dialog.querySelector(".cmd-hint").hidden = false;
    input.focus();
    await prepare();
    dialog.querySelector(".cmd-hint").textContent = "";
    render();
  }

  document.addEventListener("keydown", (e) => {
    const keyName = (e.key || "").toLowerCase();
    if (!(keyName === "k" && (e.metaKey || e.ctrlKey))) return;
    e.preventDefault();
    if (dialog && dialog.open) dialog.close();
    else show();
  });

  const trigger = document.getElementById("cmd-open");
  if (trigger) {
    trigger.title = "Search (" + key + ")";
    trigger.addEventListener("click", () => show());
  }
})();
