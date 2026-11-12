/* The firmware stand-in of a virtual display (ADR 0091): sync, paint, report
   what is shown, come back for the refresh or the one-shot wake. The server
   does the plan, the menu and the rendering; this file only presses buttons. */
(() => {
  "use strict";
  const cfg = {
    sync: document.body.dataset.sync || "",
    reset: document.body.dataset.reset || "",
  };
  const image = document.getElementById("image");
  const text = document.getElementById("text");
  const debug = document.getElementById("debug");
  const json = document.getElementById("json");

  let shown = "";   // the hash the panel shows, empty before the first paint
  let events = 0;   // monotonic event id, like the firmware's RTC counter
  let timer = 0;
  let busy = false;
  let queue = [];   // events collected while a sync runs
  let pending = ""; // a wake that arrived while a sync was running

  function say(message, error) {
    debug.textContent = message;
    debug.classList.toggle("error", !!error);
  }

  // describe names what the sync decided, which a device only logs.
  function describe(out) {
    const d = out.debug || {};
    const parts = ["rule " + (d.rule || "none")];
    if (d.page_name) {
      const count = d.pages > 1 ? "/" + d.pages : "";
      parts.push("page " + (d.page + 1) + count + " · " + d.page_name);
    } else if (d.page_kind) {
      parts.push("page " + d.page_kind);
    }
    if (d.menu) parts.push("menu " + d.menu);
    parts.push("refresh " + out.refresh_s + " s");
    if (out.wake) parts.push("wake " + out.wake.reason + " in " + out.wake.after_s + " s");
    if (out.listen_s > 0) parts.push("listen " + out.listen_s + " s");
    if (d.error) parts.push("error " + d.error);
    return parts.join(" · ");
  }

  // paint shows the answer and reports the hash only once the panel has it.
  function paint(out) {
    if (out.text) {
      text.textContent = (out.text.lines || []).join("\n");
      text.hidden = false;
      image.hidden = true;
      shown = out.text.hash;
      return Promise.resolve();
    }
    if (!out.image || out.image.hash === shown) return Promise.resolve();
    return new Promise((resolve) => {
      image.onload = () => {
        shown = out.image.hash;
        image.hidden = false;
        text.hidden = true;
        resolve();
      };
      image.onerror = () => {
        say("the image did not load: " + out.image.path, true);
        resolve();
      };
      image.src = out.image.path;
    });
  }

  function schedule(out) {
    clearTimeout(timer);
    if (out.wake && out.wake.after_s > 0) {
      timer = setTimeout(() => sync(out.wake.reason), out.wake.after_s * 1000);
      return;
    }
    const seconds = Number(out.refresh_s) || 0;
    if (seconds > 0) timer = setTimeout(() => sync("timer"), seconds * 1000);
  }

  async function sync(reason) {
    clearTimeout(timer);
    if (busy) {
      pending = reason;
      return;
    }
    busy = true;
    const body = { reason: reason, shown: shown, events: queue };
    queue = [];
    try {
      const res = await fetch(cfg.sync, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!res.ok) {
        say("sync failed: HTTP " + res.status + " · " + (await res.text()).trim(), true);
        timer = setTimeout(() => sync("timer"), 10000);
        return;
      }
      const out = await res.json();
      await paint(out);
      json.textContent = JSON.stringify(out, null, 2);
      say(describe(out), !!(out.debug && out.debug.error));
      schedule(out);
    } catch (err) {
      say("sync failed: " + err, true);
      timer = setTimeout(() => sync("timer"), 10000);
    } finally {
      busy = false;
      if (queue.length) {
        // Input during the sync becomes one follow-up wake, like the firmware.
        timer = setTimeout(() => sync("button"), 150);
      } else if (pending) {
        const reason = pending;
        pending = "";
        timer = setTimeout(() => sync(reason), 150);
      }
    }
  }

  function press(role, holdMs) {
    events += 1;
    queue.push({ id: events, type: "button", button: role, hold_ms: holdMs });
    if (busy) return;
    clearTimeout(timer);
    sync("button");
  }

  document.querySelectorAll("button[data-role]").forEach((button) => {
    let downAt = 0;
    button.addEventListener("pointerdown", (event) => {
      if (event.pointerType === "mouse" && event.button !== 0) return;
      event.preventDefault();
      downAt = performance.now();
      if (button.setPointerCapture) button.setPointerCapture(event.pointerId);
    });
    button.addEventListener("pointerup", () => {
      if (!downAt) return;
      const held = Math.round(performance.now() - downAt);
      downAt = 0;
      press(button.dataset.role, held);
    });
    button.addEventListener("pointercancel", () => { downAt = 0; });
  });

  const keys = { ArrowLeft: "previous", ArrowRight: "next", Enter: "home" };
  let keyDownAt = 0;
  document.addEventListener("keydown", (event) => {
    if (event.key === "f" && !event.repeat) {
      toggleFull();
      return;
    }
    if (!keys[event.key]) return;
    event.preventDefault();
    if (!event.repeat) keyDownAt = performance.now();
  });
  document.addEventListener("keyup", (event) => {
    if (!keys[event.key] || !keyDownAt) return;
    const held = Math.round(performance.now() - keyDownAt);
    keyDownAt = 0;
    press(keys[event.key], held);
  });
  window.addEventListener("blur", () => { keyDownAt = 0; });

  function toggleFull() {
    if (document.fullscreenElement) {
      if (document.exitFullscreen) document.exitFullscreen();
      return;
    }
    if (document.documentElement.requestFullscreen) {
      document.documentElement.requestFullscreen().catch(() => document.body.classList.toggle("show"));
      return;
    }
    document.body.classList.toggle("show");
  }
  document.getElementById("full").addEventListener("click", toggleFull);
  document.addEventListener("fullscreenchange", () => {
    document.body.classList.toggle("show", !!document.fullscreenElement);
  });

  document.getElementById("reset").addEventListener("click", async () => {
    clearTimeout(timer);
    try {
      const res = await fetch(cfg.reset, { method: "POST" });
      if (!res.ok) {
        say("reset failed: HTTP " + res.status, true);
        return;
      }
      shown = "";
      queue = [];
      image.hidden = true;
      text.hidden = true;
      say("session reset");
      sync("powercycle");
    } catch (err) {
      say("reset failed: " + err, true);
    }
  });

  sync("timer");
})();
