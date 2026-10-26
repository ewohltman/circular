// Dark mode toggle. Loaded in <head> without defer so the saved theme is set
// before first paint. With no saved choice the page follows the system
// preference through CSS; clicking the toggle saves an explicit choice.
(function () {
  var KEY = "circular-theme";
  var root = document.documentElement;
  var dark = window.matchMedia("(prefers-color-scheme: dark)");

  function saved() {
    try {
      var v = localStorage.getItem(KEY);
      return v === "light" || v === "dark" ? v : null;
    } catch (e) {
      return null;
    }
  }

  function current() {
    return saved() || (dark.matches ? "dark" : "light");
  }

  var initial = saved();
  if (initial) root.dataset.theme = initial;

  function sync(btn) {
    var isDark = current() === "dark";
    btn.setAttribute("aria-pressed", String(isDark));
    btn.title = isDark ? "Switch to light mode" : "Switch to dark mode";
  }

  document.addEventListener("DOMContentLoaded", function () {
    var btn = document.getElementById("theme-toggle");
    if (!btn) return;
    btn.hidden = false;
    sync(btn);
    btn.addEventListener("click", function () {
      var next = current() === "dark" ? "light" : "dark";
      root.dataset.theme = next;
      try {
        localStorage.setItem(KEY, next);
      } catch (e) {
        // Private mode or blocked storage: the choice lasts for this page only.
      }
      sync(btn);
    });
    // Keep the button's label right if the system theme flips while no
    // explicit choice is saved.
    dark.addEventListener("change", function () {
      sync(btn);
    });
  });
})();
