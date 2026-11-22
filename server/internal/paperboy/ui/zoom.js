// The overview preview opens in a modal at the largest size the viewport
// holds. One dialog outside the polled region survives a tick, and the image
// comes from the browser cache because it keeps the src of the card.
(() => {
  "use strict";

  const dialog = document.getElementById("zoom");
  if (!dialog) return;

  const image = dialog.querySelector("img");
  const text = dialog.querySelector("pre");
  const device = dialog.querySelector(".zoom-device");
  const page = dialog.querySelector(".zoom-page");
  const served = dialog.querySelector(".zoom-served");

  function open(button) {
    const kind = button.dataset.kind;
    const source = button.querySelector(kind === "text" ? "pre" : "img");

    image.hidden = kind === "text";
    text.hidden = kind !== "text";
    if (kind === "text") text.textContent = source.textContent;
    else image.src = source.src;

    device.textContent = button.dataset.device;
    page.textContent = button.dataset.page;
    if (button.dataset.pageUrl) page.href = button.dataset.pageUrl;
    else page.removeAttribute("href");

    served.hidden = !button.dataset.served;
    served.textContent = button.dataset.served ? "Served " + button.dataset.served : "";

    dialog.showModal();
  }

  for (const button of document.querySelectorAll("button[data-zoom]")) {
    button.addEventListener("click", () => open(button));
  }
  dialog.querySelector("[data-close]").addEventListener("click", () => dialog.close());
  dialog.addEventListener("click", (event) => {
    if (event.target === dialog) dialog.close();
  });
})();
