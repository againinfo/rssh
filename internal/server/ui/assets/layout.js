(function () {
  const html = document.documentElement;
  const sidebar = document.getElementById("sidebar");
  const layout = document.getElementById("layout");
  const toggle = document.getElementById("sidebar-toggle");
  const themeBtn = document.getElementById("theme-toggle");

  function setActiveNav() {
    const path = window.location.pathname || "";
    document.querySelectorAll("[data-nav]").forEach((a) => {
      const href = a.getAttribute("href") || "";
      const active = href === "/ui/" ? path === "/ui/" || path === "/ui" : path.startsWith(href);
      a.classList.toggle("snav__link--active", !!active);
    });
  }

  function setSidebarOpen(open) {
    if (!layout) return;
    layout.dataset.sidebar = open ? "open" : "closed";
    try {
      localStorage.setItem("rssh_sidebar", open ? "open" : "closed");
    } catch (_) {}
  }

  function restoreSidebar() {
    try {
      const v = localStorage.getItem("rssh_sidebar");
      if (v === "closed") setSidebarOpen(false);
    } catch (_) {}
  }

  function applyTheme(mode) {
    // mode: "system" | "light" | "dark"
    if (mode === "system") {
      html.removeAttribute("data-theme");
    } else {
      html.setAttribute("data-theme", mode);
    }
    try {
      localStorage.setItem("rssh_theme", mode);
    } catch (_) {}
    if (themeBtn) {
      const label = mode === "system" ? "Theme: System" : mode === "light" ? "Theme: Light" : "Theme: Dark";
      themeBtn.setAttribute("aria-label", label);
      themeBtn.title = label;
    }
  }

  function restoreTheme() {
    try {
      const v = localStorage.getItem("rssh_theme");
      if (v === "light" || v === "dark" || v === "system") applyTheme(v);
    } catch (_) {}
  }

  function cycleTheme() {
    const cur = (function () {
      const v = html.getAttribute("data-theme");
      if (!v) return "system";
      return v === "dark" ? "dark" : "light";
    })();
    const next = cur === "system" ? "light" : cur === "light" ? "dark" : "system";
    applyTheme(next);
  }

  setActiveNav();
  restoreSidebar();
  restoreTheme();

  if (toggle) toggle.addEventListener("click", () => setSidebarOpen(layout && layout.dataset.sidebar !== "open"));

  if (themeBtn) themeBtn.addEventListener("click", cycleTheme);

  // Close sidebar when clicking outside on small screens.
  document.addEventListener("click", (e) => {
    if (!layout || layout.dataset.sidebar !== "open") return;
    const t = e.target;
    if (!t) return;
    if (sidebar && sidebar.contains(t)) return;
    if (toggle && toggle.contains(t)) return;
    if (window.matchMedia && window.matchMedia("(max-width: 920px)").matches) setSidebarOpen(false);
  });
})();

