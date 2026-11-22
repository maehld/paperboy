// One dialog for every prompt and confirmation of the operator UI (ADR 0152).
// The browser supplies the modal, the backdrop, the focus trap and Escape, so
// the UI needs no overlay code. Callers get a promise:
//
//	const name = await pbAsk.text({ title: "Rename page", label: "Name", value: old });
//	if (!name) return;                                   // cancelled
//	if (!(await pbAsk.confirm({ title: "Delete?", ok: "Delete", danger: true }))) return;
(() => {
  "use strict";

  let dialog = null;

  function build() {
    const box = document.createElement("dialog");
    box.className = "ask";
    box.innerHTML = `
      <form method="dialog">
        <h2 class="ask-title"></h2>
        <p class="ask-text"></p>
        <label class="ask-field"><span class="ask-label"></span><input name="value" autocomplete="off"></label>
        <p class="ask-hint"></p>
        <div class="ask-actions">
          <button type="submit" value="cancel" class="ask-cancel">Cancel</button>
          <button type="submit" value="ok" class="ask-ok"></button>
        </div>
      </form>`;
    document.body.appendChild(box);
    return box;
  }

  function open(opts) {
    if (!dialog) dialog = build();
    const ask = opts.value !== undefined;
    dialog.querySelector(".ask-title").textContent = opts.title || "";
    const text = dialog.querySelector(".ask-text");
    text.textContent = opts.text || "";
    text.hidden = !opts.text;
    const field = dialog.querySelector(".ask-field");
    field.hidden = !ask;
    const input = dialog.querySelector("input");
    const ok = dialog.querySelector(".ask-ok");
    ok.textContent = opts.ok || (ask ? "OK" : "Continue");
    ok.className = "ask-ok " + (opts.danger ? "danger" : "primary");

    if (ask) {
      field.querySelector(".ask-label").textContent = opts.label || "";
      input.value = opts.value;
      input.placeholder = opts.placeholder || "";
      if (opts.pattern) input.pattern = opts.pattern;
      else input.removeAttribute("pattern");
      const sync = () => {
        ok.disabled = !input.value.trim() || !input.checkValidity();
      };
      input.oninput = sync;
      sync();
    }

    const hint = dialog.querySelector(".ask-hint");
    hint.textContent = opts.hint || "";
    hint.hidden = !opts.hint;

    return new Promise((resolve) => {
      dialog.returnValue = "";
      dialog.addEventListener(
        "close",
        () => {
          if (dialog.returnValue !== "ok") {
            resolve(ask ? null : false);
            return;
          }
          resolve(ask ? input.value.trim() : true);
        },
        { once: true },
      );
      dialog.showModal();
      if (ask) {
        input.focus();
        input.select();
      } else {
        ok.focus();
      }
    });
  }

  window.pbAsk = {
    // text asks for one line and answers null when the operator cancels.
    text(opts) {
      return open(Object.assign({ value: "" }, opts));
    },
    // confirm answers whether the operator agreed; danger reddens the button.
    confirm(opts) {
      return open(opts);
    },
  };
})();
