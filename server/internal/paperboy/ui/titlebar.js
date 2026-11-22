// On a narrow screen the title bar keeps one primary action and moves the rest
// into one menu. Moving a node keeps its listeners and its hidden state, so the
// editors need no second action set.
(() => {
  "use strict";

  const actions = document.querySelector(".title-actions");
  const more = document.getElementById("action-more");
  const menu = document.getElementById("action-menu");
  if (!actions || !more || !menu) return;

  const narrow = window.matchMedia("(max-width: 40rem)");
  let original = null;

  function close() {
    menu.hidden = true;
    more.setAttribute("aria-expanded", "false");
  }

  function open() {
    menu.hidden = false;
    more.setAttribute("aria-expanded", "true");
  }

  function apply() {
    if (narrow.matches) {
      if (!original) original = Array.from(actions.children);
      menu.replaceChildren();
      const keep = new Set(original.filter((node) => node.matches(".primary, .new")));
      if (keep.size === 0 && original.length) keep.add(original[original.length - 1]);
      for (const node of original) {
        if (keep.has(node)) continue;
        node.setAttribute("role", "menuitem");
        menu.append(node);
      }
      more.hidden = menu.children.length === 0;
      if (more.hidden) close();
      return;
    }
    close();
    if (original) {
      actions.replaceChildren(...original);
      original = null;
    }
    more.hidden = true;
  }

  more.addEventListener("click", (event) => {
    event.stopPropagation();
    if (menu.hidden) open();
    else close();
  });
  menu.addEventListener("click", close);
  document.addEventListener("click", (event) => {
    if (!event.target.closest(".action-overflow")) close();
  });
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape") close();
  });
  narrow.addEventListener("change", apply);
  apply();
})();
