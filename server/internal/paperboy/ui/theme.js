// The theme toggle of ADR 0152. The choice lives in the browser only: without a
// stored value the page follows the operating system, and a click stores the
// opposite of what the page shows now. tokens.css turns data-theme into a
// colour-scheme, so the whole palette follows one attribute.
const toggle = document.getElementById("theme-toggle");
const root = document.documentElement;
const system = window.matchMedia("(prefers-color-scheme: dark)");

function shown() {
  if (root.dataset.theme === "dark" || root.dataset.theme === "light") return root.dataset.theme;
  return system.matches ? "dark" : "light";
}

function render() {
  if (!toggle) return;
  const next = shown() === "dark" ? "light" : "dark";
  // The label names what the click does, so the glyph of the button cannot
  // disagree with the theme.
  const label = toggle.querySelector(".theme-label");
  if (label) label.textContent = next === "dark" ? "Dark" : "Light";
  toggle.setAttribute("aria-label", "Switch to the " + next + " theme");
  toggle.setAttribute("title", "Switch to the " + next + " theme");
}

if (toggle) {
  toggle.addEventListener("click", () => {
    const next = shown() === "dark" ? "light" : "dark";
    root.dataset.theme = next;
    try {
      localStorage.setItem("pb-theme", next);
    } catch {
      // A browser that refuses storage keeps the choice for this page only.
    }
    render();
  });
  render();
}
