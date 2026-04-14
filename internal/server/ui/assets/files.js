(async function () {
  const meta = document.getElementById("client-meta");
  const id = (meta && meta.dataset && meta.dataset.clientId) || "";

  const pathEl = document.getElementById("path");
  const go = document.getElementById("go");
  const up = document.getElementById("up");
  const refresh = document.getElementById("refresh");
  const tree = document.getElementById("tree");
  const tbody = document.getElementById("files");
  const status = document.getElementById("fs-status");
  const filterEl = document.getElementById("filter");
  const crumbsEl = document.getElementById("breadcrumbs");
  const upload = document.getElementById("upload");
  const mkdir = document.getElementById("mkdir");

  // Modal + editor/viewer
  const modal = document.getElementById("editor-modal");
  const backdrop = document.getElementById("modal-backdrop");
  const tabsEl = document.getElementById("edit-tabs");
  const editPath = document.getElementById("edit-path");
  const editHighlight = document.getElementById("edit-highlight");
  const editPrev = document.getElementById("edit-prev");
  const editNext = document.getElementById("edit-next");
  const editReload = document.getElementById("edit-reload");
  const editSave = document.getElementById("edit-save");
  const editClose = document.getElementById("edit-close");
  const editHint = document.getElementById("edit-hint");
  const editArea = document.getElementById("edit-area");
  const editPre = document.getElementById("edit-pre");
  const editImage = document.getElementById("edit-image");

  // Drag/drop upload
  const dropzone = document.getElementById("dropzone");
  const uploadProgress = document.getElementById("upload-progress");
  const uploadProgressBar = document.getElementById("upload-progress-bar");
  const dropProgress = document.getElementById("drop-progress");
  const dropProgressBar = document.getElementById("drop-progress-bar");

  const LS_KEY_PATH = `rssh_files_last_path:${id}`;
  const LS_KEY_EXPANDED = `rssh_files_tree_expanded:${id}`;
  const LS_KEY_FILTER = `rssh_files_filter:${id}`;

  const expandedDirs = new Set();
  const treeNodesByPath = new Map();

  let lastListedEntries = [];

  const tabs = []; // {id, path, kind, editable, offset, max, encoding, truncated, size, readOnly, highlightOn, content}
  let activeTabId = null;

  function esc(str) {
    return String(str).replace(/[&<>"']/g, (c) => ({
      "&": "&amp;",
      "<": "&lt;",
      ">": "&gt;",
      '"': "&quot;",
      "'": "&#39;",
    }[c]));
  }

  function setStatus(text) {
    if (status) status.textContent = text || "";
  }

  function parentDir(p) {
    p = (p || "/").replace(/\/+$/, "");
    const idx = p.lastIndexOf("/");
    if (idx <= 0) return "/";
    return p.slice(0, idx);
  }

  function applyFilter(entries) {
    const q = (filterEl && filterEl.value ? filterEl.value : "").trim().toLowerCase();
    if (!q) return entries;
    return (entries || []).filter((e) => String(e.name || "").toLowerCase().includes(q));
  }

  if (filterEl) {
    filterEl.addEventListener("input", () => {
      try {
        if (id) localStorage.setItem(LS_KEY_FILTER, filterEl.value || "");
      } catch (_) {}
    });
  }

  function isEditableCandidate(entry) {
    if (!entry || entry.is_dir) return false;
    const name = entry.name || "";
    const size = entry.size || 0;
    if (size > 512 * 1024) return false;
    return /\.(txt|log|md|json|yaml|yml|toml|ini|conf|cfg|sh|py|go|js|ts|tsx|jsx|css|html|xml|sql|env|properties|java|c|cc|cpp|h|hpp|rs|rb|php)$/i.test(
      name
    );
  }

  function isSafeImageCandidate(entry) {
    if (!entry || entry.is_dir) return false;
    const name = entry.name || "";
    return /\.(png|jpe?g|gif|webp|bmp|ico|svg)$/i.test(name);
  }

  function formatOwner(entry) {
    if (!entry || entry.is_dir) return "";
    const uid = typeof entry.uid === "number" ? entry.uid : 0;
    const gid = typeof entry.gid === "number" ? entry.gid : 0;
    if (!uid && !gid) return "";
    return `${uid}:${gid}`;
  }

  async function fetchList(p) {
    const res = await fetch(`/ui/api/clients/${encodeURIComponent(id)}/fs/list?path=${encodeURIComponent(p)}`);
    const data = await res.json().catch(() => null);
    if (!data || !data.ok) throw new Error((data && data.error) || "list failed");
    return data.entries || [];
  }

  function renderBreadcrumbs(p) {
    if (!crumbsEl) return;
    const clean = (p || "/").trim() || "/";
    const parts = clean.replace(/\/+$/, "").split("/").filter(Boolean);
    const wrap = document.createElement("div");
    wrap.className = "crumbs";

    function addCrumb(label, path) {
      const a = document.createElement("a");
      a.className = "crumbs__a";
      a.href = "#";
      a.textContent = label;
      a.addEventListener("click", async (ev) => {
        ev.preventDefault();
        pathEl.value = path;
        await list();
      });
      wrap.appendChild(a);
    }

    addCrumb("/", "/");
    let cur = "";
    parts.forEach((seg) => {
      cur += "/" + seg;
      const sep = document.createElement("span");
      sep.className = "crumbs__sep";
      sep.textContent = " / ";
      wrap.appendChild(sep);
      addCrumb(seg, cur);
    });

    crumbsEl.innerHTML = "";
    crumbsEl.appendChild(wrap);
  }

  async function list() {
    const p = (pathEl.value || "/").trim() || "/";
    setStatus("Loading…");
    tbody.innerHTML = "";
    let entries;
    try {
      entries = await fetchList(p);
    } catch (err) {
      setStatus(err && err.message ? err.message : "error");
      return;
    }

    lastListedEntries = entries;
    renderBreadcrumbs(p);
    try {
      if (id) localStorage.setItem(LS_KEY_PATH, p);
    } catch (_) {}

    const filtered = applyFilter(entries);
    filtered.forEach((e) => {
      const tr = document.createElement("tr");
      const name = e.name || "";
      const isDir = !!e.is_dir;
      const canEdit = isEditableCandidate(e);
      const canImage = isSafeImageCandidate(e);

      tr.innerHTML = `
        <td>
          <span class="badge">${isDir ? "DIR" : "FILE"}</span>
          <a href="#" class="mono">${esc(name)}</a>
        </td>
        <td class="mono">${isDir ? "" : String(e.size || 0)}</td>
        <td class="mono">${esc(e.mode || "")}</td>
        <td class="mono">${esc(formatOwner(e))}</td>
        <td class="mono">${esc(String(e.mod_time || ""))}</td>
        <td class="row">
          ${isDir ? "" : `<a class="btn btn--ghost btn--sm" href="/ui/api/clients/${encodeURIComponent(id)}/fs/download?path=${encodeURIComponent(e.path)}">Download</a>`}
          <button class="btn btn--ghost btn--sm del-btn" type="button">Delete</button>
        </td>
      `;

      tr.querySelector("a").addEventListener("click", (ev) => {
        ev.preventDefault();
        if (isDir) {
          pathEl.value = e.path;
          list();
          return;
        }
        openTab(e, { editable: canEdit, preferImage: canImage });
      });

      tr.querySelector(".del-btn").addEventListener("click", async () => {
        if (!confirm(`Delete ${e.path}?`)) return;
        const res = await fetch(`/ui/api/clients/${encodeURIComponent(id)}/fs/rm`, {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ path: e.path, recursive: isDir }),
        });
        const d = await res.json().catch(() => null);
        if (!d || !d.ok) alert((d && d.error) || "failed");
        await list();
        await refreshTree();
      });

      tr.addEventListener("contextmenu", (ev) => {
        ev.preventDefault();
        showContextMenu(ev.clientX, ev.clientY, e, { isDir, canEdit, canImage });
      });

      tbody.appendChild(tr);
    });

    if (filtered.length === 0) {
      const tr = document.createElement("tr");
      tr.innerHTML = `<td class="mono" colspan="6">${entries.length === 0 ? "(empty)" : "(no match)"}</td>`;
      tbody.appendChild(tr);
    }
    setStatus("");
  }

  function listFromCache() {
    const p = (pathEl.value || "/").trim() || "/";
    renderBreadcrumbs(p);
    tbody.innerHTML = "";
    const entries = lastListedEntries || [];
    const filtered = applyFilter(entries);
    filtered.forEach((e) => {
      const tr = document.createElement("tr");
      const name = e.name || "";
      const isDir = !!e.is_dir;
      const canEdit = isEditableCandidate(e);
      const canImage = isSafeImageCandidate(e);

      tr.innerHTML = `
        <td>
          <span class="badge">${isDir ? "DIR" : "FILE"}</span>
          <a href="#" class="mono">${esc(name)}</a>
        </td>
        <td class="mono">${isDir ? "" : String(e.size || 0)}</td>
        <td class="mono">${esc(e.mode || "")}</td>
        <td class="mono">${esc(formatOwner(e))}</td>
        <td class="mono">${esc(String(e.mod_time || ""))}</td>
        <td class="row">
          ${isDir ? "" : `<a class="btn btn--ghost btn--sm" href="/ui/api/clients/${encodeURIComponent(id)}/fs/download?path=${encodeURIComponent(e.path)}">Download</a>`}
          <button class="btn btn--ghost btn--sm del-btn" type="button">Delete</button>
        </td>
      `;

      tr.querySelector("a").addEventListener("click", (ev) => {
        ev.preventDefault();
        if (isDir) {
          pathEl.value = e.path;
          list();
          return;
        }
        openTab(e, { editable: canEdit, preferImage: canImage });
      });

      tr.querySelector(".del-btn").addEventListener("click", async () => {
        if (!confirm(`Delete ${e.path}?`)) return;
        const res = await fetch(`/ui/api/clients/${encodeURIComponent(id)}/fs/rm`, {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ path: e.path, recursive: isDir }),
        });
        const d = await res.json().catch(() => null);
        if (!d || !d.ok) alert((d && d.error) || "failed");
        await list();
        await refreshTree();
      });

      tr.addEventListener("contextmenu", (ev) => {
        ev.preventDefault();
        showContextMenu(ev.clientX, ev.clientY, e, { isDir, canEdit, canImage });
      });

      tbody.appendChild(tr);
    });

    if (filtered.length === 0) {
      const tr = document.createElement("tr");
      tr.innerHTML = `<td class="mono" colspan="6">${entries.length === 0 ? "(empty)" : "(no match)"}</td>`;
      tbody.appendChild(tr);
    }
  }

  // Context menu
  const menu = document.createElement("div");
  menu.className = "ctx";
  menu.style.display = "none";
  document.body.appendChild(menu);

  function hideMenu() {
    menu.style.display = "none";
    menu.innerHTML = "";
  }
  window.addEventListener("click", hideMenu);
  window.addEventListener("scroll", hideMenu, true);
  window.addEventListener("keydown", (e) => {
    if (e.key === "Escape") hideMenu();
  });

  function addItem(label, icon, onClick, danger) {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "ctx__item" + (danger ? " ctx__danger" : "");
    btn.innerHTML = `<span class="ctx__ico ico--${icon.replace(/[^a-z0-9-]/gi, "")}" aria-hidden="true"></span>${esc(label)}`;
    btn.addEventListener("click", () => {
      hideMenu();
      onClick();
    });
    menu.appendChild(btn);
    return btn;
  }

  function showContextMenu(x, y, entry, flags) {
    menu.innerHTML = "";

    if (flags.isDir) {
      addItem("Open", "folder", () => {
        pathEl.value = entry.path;
        list();
      });
      addItem("Rename", "pencil", async () => {
        const to = prompt("Rename to", entry.path);
        if (!to || to === entry.path) return;
        const res = await fetch(`/ui/api/clients/${encodeURIComponent(id)}/fs/rename`, {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ from: entry.path, to }),
        });
        const d = await res.json().catch(() => null);
        if (!d || !d.ok) alert((d && d.error) || "rename failed");
        await list();
        await refreshTree();
      });
    } else {
      if (flags.canImage) {
        addItem("Preview image", "file", () => openTab(entry, { editable: false, preferImage: true }), false);
      }
      addItem("View", "file", () => openTab(entry, { editable: flags.canEdit, preferImage: false }), false);
      addItem("Download", "download", () => {
        window.location.href = `/ui/api/clients/${encodeURIComponent(id)}/fs/download?path=${encodeURIComponent(entry.path)}`;
      });
      addItem("Rename", "pencil", async () => {
        const to = prompt("Rename to", entry.path);
        if (!to || to === entry.path) return;
        const res = await fetch(`/ui/api/clients/${encodeURIComponent(id)}/fs/rename`, {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ from: entry.path, to }),
        });
        const d = await res.json().catch(() => null);
        if (!d || !d.ok) alert((d && d.error) || "rename failed");
        await list();
        await refreshTree();
      });
      if (flags.canEdit) {
        addItem("Edit", "pencil", () => openTab(entry, { editable: true, preferImage: false }), false);
      } else {
        const btn = addItem("Edit (limited)", "pencil", () => alert("Editing is enabled only for common text files up to 512KB."), false);
        btn.setAttribute("disabled", "disabled");
      }
    }

    addItem(
      "Delete",
      "trash",
      async () => {
        if (!confirm(`Delete ${entry.path}?`)) return;
        const res = await fetch(`/ui/api/clients/${encodeURIComponent(id)}/fs/rm`, {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ path: entry.path, recursive: flags.isDir }),
        });
        const d = await res.json().catch(() => null);
        if (!d || !d.ok) alert((d && d.error) || "delete failed");
        await list();
        await refreshTree();
      },
      true
    );

    menu.style.display = "block";
    const rect = menu.getBoundingClientRect();
    const maxX = window.innerWidth - rect.width - 10;
    const maxY = window.innerHeight - rect.height - 10;
    menu.style.left = Math.max(10, Math.min(x, maxX)) + "px";
    menu.style.top = Math.max(10, Math.min(y, maxY)) + "px";
  }

  // Tabs + modal
  function showModal() {
    if (!modal) return;
    modal.style.display = "block";
    modal.setAttribute("aria-hidden", "false");
    document.body.style.overflow = "hidden";
  }

  function hideModal() {
    if (!modal) return;
    modal.style.display = "none";
    modal.setAttribute("aria-hidden", "true");
    document.body.style.overflow = "";
  }

  function tabLabel(path) {
    const p = String(path || "");
    const parts = p.split("/").filter(Boolean);
    return parts.length ? parts[parts.length - 1] : p || "/";
  }

  function getActiveTab() {
    return tabs.find((t) => t.id === activeTabId) || null;
  }

  function renderTabs() {
    if (!tabsEl) return;
    tabsEl.innerHTML = "";
    tabs.forEach((t) => {
      const el = document.createElement("div");
      el.className = "tab" + (t.id === activeTabId ? " tab--active" : "");
      el.innerHTML = `<span class="mono">${esc(tabLabel(t.path))}</span><button class="tab__x" type="button" aria-label="Close">×</button>`;
      el.addEventListener("click", () => {
        setActiveTab(t.id);
      });
      el.querySelector(".tab__x").addEventListener("click", (ev) => {
        ev.stopPropagation();
        closeTab(t.id);
      });
      tabsEl.appendChild(el);
    });
  }

  function setActiveTab(tabId) {
    activeTabId = tabId;
    renderTabs();
    renderActiveTab();
  }

  function closeTab(tabId) {
    const idx = tabs.findIndex((t) => t.id === tabId);
    if (idx === -1) return;
    tabs.splice(idx, 1);
    if (activeTabId === tabId) {
      activeTabId = tabs.length ? tabs[Math.max(0, idx - 1)].id : null;
    }
    renderTabs();
    if (!activeTabId) {
      hideModal();
      return;
    }
    renderActiveTab();
  }

  function setViewMode(mode) {
    if (editImage) editImage.style.display = mode === "image" ? "block" : "none";
    if (editPre) editPre.style.display = mode === "pre" ? "block" : "none";
    if (editArea) editArea.style.display = mode === "area" ? "block" : "none";
  }

  function updatePagerUI(t) {
    if (!t || t.kind !== "text") {
      if (editPrev) editPrev.disabled = true;
      if (editNext) editNext.disabled = true;
      return;
    }
    if (editPrev) editPrev.disabled = t.offset <= 0;
    if (editNext) editNext.disabled = !t.truncated;
  }

  function updateHighlightUI(t) {
    if (!editHighlight) return;
    const ok = t && t.kind === "text" && t.encoding === "utf-8";
    editHighlight.disabled = !ok;
    editHighlight.textContent = t && t.highlightOn ? "Plain" : "Highlight";
  }

  function renderHighlighted(text) {
    const lines = String(text || "").split("\n");
    const out = lines
      .map((line) => {
        const s = esc(line);
        if (/(^|\\b)(error|fatal|panic)\\b/i.test(line)) return `<span class="hl-error">${s}</span>`;
        if (/(^|\\b)(warn|warning)\\b/i.test(line)) return `<span class="hl-warn">${s}</span>`;
        if (/(^|\\b)(info)\\b/i.test(line)) return `<span class="hl-info">${s}</span>`;
        return s;
      })
      .join("\n");
    return out;
  }

  async function renderActiveTab() {
    const t = getActiveTab();
    if (!t) return;

    if (editPath) editPath.textContent = t.path;
    if (editHint) editHint.textContent = "Loading…";

    if (t.kind === "image") {
      setViewMode("image");
      if (editImage) {
        editImage.src = `/ui/api/clients/${encodeURIComponent(id)}/fs/open?path=${encodeURIComponent(t.path)}`;
      }
      if (editSave) editSave.disabled = true;
      if (editReload) editReload.disabled = false;
      updatePagerUI(null);
      updateHighlightUI(null);
      if (editHint) editHint.textContent = "image preview";
      return;
    }

    // text/hex
    await loadTabChunk(t);
  }

  async function loadTabChunk(t) {
    if (!t) return;
    const url = `/ui/api/clients/${encodeURIComponent(id)}/fs/read?path=${encodeURIComponent(t.path)}&offset=${encodeURIComponent(
      String(t.offset || 0)
    )}&max=${encodeURIComponent(String(t.max || 512 * 1024))}`;
    const res = await fetch(url);
    const data = await res.json().catch(() => null);
    if (!data || !data.ok) {
      if (editHint) editHint.textContent = (data && data.error) || "read failed";
      return;
    }

    t.content = data.content || "";
    t.encoding = data.encoding || "";
    t.truncated = !!data.truncated;
    t.offset = data.offset || 0;
    t.max = data.max || t.max;
    t.size = (data.meta && data.meta.size) || 0;
    t.readOnly = !!data.read_only;

    // Decide view mode
    const canSave = t.editable && !t.readOnly && t.encoding === "utf-8";
    if (editSave) editSave.disabled = !canSave;
    if (editArea) editArea.readOnly = !canSave;

    updatePagerUI(t);
    updateHighlightUI(t);

    if (t.encoding !== "utf-8") {
      setViewMode("pre");
      if (editPre) editPre.textContent = t.content;
    } else if (t.highlightOn) {
      setViewMode("pre");
      if (editPre) editPre.innerHTML = renderHighlighted(t.content);
    } else {
      setViewMode("area");
      if (editArea) editArea.value = t.content;
    }

    if (editHint) {
      const parts = [];
      if (t.encoding) parts.push(`encoding=${t.encoding}`);
      if (t.size) parts.push(`size=${t.size}`);
      parts.push(`offset=${t.offset}`);
      parts.push(`max=${t.max}`);
      if (t.truncated) parts.push("more=YES");
      if (!canSave) parts.push("mode=read-only");
      editHint.textContent = parts.join("  ");
    }
  }

  function ensureTab(path, kind, editable) {
    const existing = tabs.find((t) => t.path === path);
    if (existing) {
      existing.kind = kind || existing.kind;
      existing.editable = !!editable;
      return existing;
    }
    const t = {
      id: `${Date.now()}-${Math.random().toString(16).slice(2)}`,
      path,
      kind: kind || "text",
      editable: !!editable,
      offset: 0,
      max: 512 * 1024,
      encoding: "",
      truncated: false,
      size: 0,
      readOnly: true,
      highlightOn: false,
      content: "",
    };
    tabs.push(t);
    return t;
  }

  function openTab(entry, opts) {
    const p = entry && entry.path ? entry.path : String(entry || "");
    const preferImage = !!(opts && opts.preferImage);
    const editable = !!(opts && opts.editable);
    const kind = preferImage && isSafeImageCandidate(entry) ? "image" : "text";
    const t = ensureTab(p, kind, editable);
    if (opts && typeof opts.offset === "number") t.offset = opts.offset;
    if (opts && typeof opts.max === "number") t.max = opts.max;
    showModal();
    activeTabId = t.id;
    renderTabs();
    renderActiveTab();
  }

  async function saveActiveTab() {
    const t = getActiveTab();
    if (!t) return;
    if (t.kind !== "text") return;
    if (!t.editable || t.readOnly || t.encoding !== "utf-8") return;
    if (editSave) editSave.disabled = true;
    if (editHint) editHint.textContent = "Saving…";
    const res = await fetch(`/ui/api/clients/${encodeURIComponent(id)}/fs/write`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ path: t.path, content: (editArea && editArea.value) || "" }),
    });
    const data = await res.json().catch(() => null);
    if (!data || !data.ok) {
      if (editHint) editHint.textContent = (data && data.error) || "save failed";
      if (editSave) editSave.disabled = false;
      return;
    }
    if (editHint) editHint.textContent = "Saved.";
    if (editSave) editSave.disabled = false;
    await list();
  }

  // Modal controls
  if (editClose)
    editClose.addEventListener("click", () => {
      hideModal();
    });
  if (backdrop)
    backdrop.addEventListener("click", () => {
      hideModal();
    });
  if (editReload)
    editReload.addEventListener("click", async () => {
      const t = getActiveTab();
      if (!t) return;
      if (t.kind === "image") {
        if (editImage) editImage.src = `/ui/api/clients/${encodeURIComponent(id)}/fs/open?path=${encodeURIComponent(t.path)}&t=${Date.now()}`;
        return;
      }
      await loadTabChunk(t);
    });
  if (editSave) editSave.addEventListener("click", saveActiveTab);
  if (editPrev)
    editPrev.addEventListener("click", async () => {
      const t = getActiveTab();
      if (!t || t.kind !== "text") return;
      t.offset = Math.max(0, (t.offset || 0) - (t.max || 512 * 1024));
      await loadTabChunk(t);
    });
  if (editNext)
    editNext.addEventListener("click", async () => {
      const t = getActiveTab();
      if (!t || t.kind !== "text") return;
      if (!t.truncated) return;
      t.offset = (t.offset || 0) + (t.max || 512 * 1024);
      await loadTabChunk(t);
    });
  if (editHighlight)
    editHighlight.addEventListener("click", async () => {
      const t = getActiveTab();
      if (!t || t.kind !== "text" || t.encoding !== "utf-8") return;
      t.highlightOn = !t.highlightOn;
      await loadTabChunk(t);
    });

  window.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && modal && modal.style.display !== "none") {
      hideModal();
      return;
    }
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s" && modal && modal.style.display !== "none") {
      e.preventDefault();
      saveActiveTab();
    }
  });

  // Drag/drop upload
  function showDropzone() {
    if (!dropzone) return;
    dropzone.style.display = "flex";
    dropzone.setAttribute("aria-hidden", "false");
  }
  function hideDropzone() {
    if (!dropzone) return;
    dropzone.style.display = "none";
    dropzone.setAttribute("aria-hidden", "true");
  }

  async function uploadFiles(files, source) {
    const dir = (pathEl.value || "/").trim() || "/";
    const listFiles = Array.from(files || []).filter((f) => f && f.name);
    if (listFiles.length === 0) return;

    const which = source === "drop" ? "drop" : "page";

    function showProg() {
      const el = which === "drop" ? dropProgress : uploadProgress;
      const bar = which === "drop" ? dropProgressBar : uploadProgressBar;
      if (el) {
        el.style.display = "block";
        el.setAttribute("aria-hidden", "false");
      }
      if (bar) bar.style.width = "0%";
    }
    function hideProg() {
      const el = which === "drop" ? dropProgress : uploadProgress;
      const bar = which === "drop" ? dropProgressBar : uploadProgressBar;
      if (bar) bar.style.width = "0%";
      if (el) {
        el.style.display = "none";
        el.setAttribute("aria-hidden", "true");
      }
    }
    function setProg(pct) {
      const bar = which === "drop" ? dropProgressBar : uploadProgressBar;
      if (!bar) return;
      const p = Math.max(0, Math.min(100, pct || 0));
      bar.style.width = `${p.toFixed(1)}%`;
    }

    function xhrUploadFile(url, file, onProgress) {
      return new Promise((resolve, reject) => {
        const xhr = new XMLHttpRequest();
        xhr.open("POST", url);
        xhr.responseType = "json";
        xhr.upload.addEventListener("progress", (ev) => {
          if (ev.lengthComputable && typeof onProgress === "function") onProgress(ev.loaded, ev.total);
        });
        xhr.addEventListener("load", () => {
          const data = xhr.response;
          if (!data || !data.ok) {
            reject(new Error((data && data.error) || "upload failed"));
            return;
          }
          resolve(data);
        });
        xhr.addEventListener("error", () => reject(new Error("network error")));
        const fd = new FormData();
        fd.append("file", file, file.name);
        xhr.send(fd);
      });
    }

    const totalBytes = listFiles.reduce((a, f) => a + (f.size || 0), 0) || 1;
    let doneBytes = 0;
    showProg();

    for (let i = 0; i < listFiles.length; i++) {
      const f = listFiles[i];
      setStatus(`Uploading ${i + 1}/${listFiles.length}: ${f.name}…`);
      const url = `/ui/api/clients/${encodeURIComponent(id)}/fs/upload?path=${encodeURIComponent(dir)}`;
      await xhrUploadFile(url, f, (loaded) => {
        const pct = ((doneBytes + loaded) / totalBytes) * 100;
        setProg(pct);
      });
      doneBytes += f.size || 0;
      setProg((doneBytes / totalBytes) * 100);
    }

    setStatus("");
    hideProg();
    await list();
    await refreshTree();
  }

  (function wireDragDrop() {
    if (!dropzone) return;
    let dragDepth = 0;

    function hasFiles(ev) {
      const dt = ev && ev.dataTransfer;
      if (!dt || !dt.types) return false;
      return Array.from(dt.types).includes("Files");
    }

    window.addEventListener("dragenter", (ev) => {
      if (!hasFiles(ev)) return;
      dragDepth++;
      showDropzone();
      ev.preventDefault();
    });
    window.addEventListener("dragover", (ev) => {
      if (!hasFiles(ev)) return;
      ev.preventDefault();
    });
    window.addEventListener("dragleave", (ev) => {
      if (!hasFiles(ev)) return;
      dragDepth = Math.max(0, dragDepth - 1);
      if (dragDepth === 0) hideDropzone();
    });
    window.addEventListener("drop", async (ev) => {
      if (!hasFiles(ev)) return;
      ev.preventDefault();
      dragDepth = 0;
      hideDropzone();
      try {
        await uploadFiles(ev.dataTransfer.files, "drop");
      } catch (e) {
        alert(e && e.message ? e.message : "upload failed");
      }
    });

    dropzone.addEventListener("click", hideDropzone);
  })();

  // Buttons
  if (go) go.addEventListener("click", list);
  if (refresh)
    refresh.addEventListener("click", async () => {
      await list();
      await refreshTree();
    });
  if (up)
    up.addEventListener("click", () => {
      pathEl.value = parentDir(pathEl.value);
      list();
    });
  if (filterEl) filterEl.addEventListener("input", listFromCache);

  if (mkdir)
    mkdir.addEventListener("click", async () => {
      const name = prompt("Directory name");
      if (!name) return;
      const base = (pathEl.value || "/").trim() || "/";
      const p = (base.replace(/\/+$/, "") || "") + "/" + name;
      const res = await fetch(`/ui/api/clients/${encodeURIComponent(id)}/fs/mkdir`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ path: p }),
      });
      const d = await res.json().catch(() => null);
      if (!d || !d.ok) alert((d && d.error) || "failed");
      await list();
      await refreshTree();
    });

  if (upload)
    upload.addEventListener("change", async () => {
      const files = (upload.files && Array.from(upload.files)) || [];
      if (files.length === 0) return;
      try {
        await uploadFiles(files, "page");
      } catch (e) {
        alert(e && e.message ? e.message : "upload failed");
      } finally {
        upload.value = "";
      }
    });

  // Tree
  async function refreshTree() {
    if (!tree) return;
    tree.innerHTML = "";
    treeNodesByPath.clear();
    const rootEntry = { name: "/", path: "/", is_dir: true, size: 0, mode: "", mod_time: "" };
    const root = renderTreeEntry(rootEntry, true);
    tree.appendChild(root.wrap);
    await expandDir(rootEntry.path, root);
    root.state.expanded = true;
    root.state.children.style.display = "block";
    if (root.twisty) root.twisty.textContent = "−";

    const desired = Array.from(expandedDirs.values()).sort((a, b) => a.length - b.length);
    for (const p of desired) {
      await expandPath(p);
    }
  }

  function renderTreeEntry(entry, isRoot) {
    const wrap = document.createElement("div");
    if (entry && entry.path) wrap.dataset.treePath = entry.path;
    const node = document.createElement("div");
    node.className = "tree__node";

    const twisty = document.createElement("span");
    twisty.className = "tree__twisty" + (!entry.is_dir ? " tree__twisty--blank" : "");
    twisty.textContent = entry.is_dir ? "+" : "";

    const label = document.createElement("span");
    label.className = "tree__label";
    label.textContent = isRoot ? "/" : entry.name || "";

    node.appendChild(twisty);
    node.appendChild(label);

    const children = document.createElement("div");
    children.className = "tree__children";
    children.style.display = "none";

    wrap.appendChild(node);
    wrap.appendChild(children);

    const state = { expanded: false, loaded: false, children };
    if (entry && entry.path) treeNodesByPath.set(entry.path, { entry, node, twisty, state });

    function setActive() {
      if (!tree) return;
      tree.querySelectorAll(".tree__node--active").forEach((n) => n.classList.remove("tree__node--active"));
      node.classList.add("tree__node--active");
    }

    node.addEventListener("click", async () => {
      if (entry.is_dir) {
        setActive();
        pathEl.value = entry.path;
        await list();
        await toggleDir(entry.path, { twisty, state });
        return;
      }
      setActive();
      // Files: single-click selects only. Use right-click context menu to view/edit.
    });

    node.addEventListener("contextmenu", (ev) => {
      ev.preventDefault();
      const canEdit = isEditableCandidate(entry);
      const canImage = isSafeImageCandidate(entry);
      showContextMenu(ev.clientX, ev.clientY, entry, { isDir: !!entry.is_dir, canEdit, canImage });
    });

    twisty.addEventListener("click", async (ev) => {
      ev.stopPropagation();
      if (!entry.is_dir) return;
      await toggleDir(entry.path, { twisty, state });
    });

    return { wrap, node, twisty, state };
  }

  async function expandDir(dirPath, treeNode) {
    if (!treeNode || !treeNode.state) return;
    const { state, twisty } = treeNode;
    if (state.loaded) return;
    state.loaded = true;
    try {
      const entries = await fetchList(dirPath);
      state.children.innerHTML = "";
      entries.forEach((e) => {
        const n = renderTreeEntry(e, false);
        state.children.appendChild(n.wrap);
      });
      if (entries.length === 0) {
        const empty = document.createElement("div");
        empty.className = "hint";
        empty.textContent = "(empty)";
        state.children.appendChild(empty);
      }
    } catch (err) {
      state.children.innerHTML = "";
      const hint = document.createElement("div");
      hint.className = "hint";
      hint.textContent = err && err.message ? err.message : "failed to load";
      state.children.appendChild(hint);
    }
    if (twisty) twisty.textContent = state.expanded ? "−" : "+";
  }

  async function toggleDir(dirPath, ctx) {
    const twisty = ctx && ctx.twisty;
    const state = ctx && ctx.state;
    if (!state) return;
    if (!state.loaded) await expandDir(dirPath, { state, twisty });
    state.expanded = !state.expanded;
    state.children.style.display = state.expanded ? "block" : "none";
    if (twisty) twisty.textContent = state.expanded ? "−" : "+";

    if (state.expanded) expandedDirs.add(dirPath);
    else expandedDirs.delete(dirPath);
    try {
      if (id) localStorage.setItem(LS_KEY_EXPANDED, JSON.stringify(Array.from(expandedDirs.values())));
    } catch (_) {}
  }

  async function expandPath(p) {
    if (!p || p === "/") return;
    const parts = p.replace(/\/+$/, "").split("/").filter(Boolean);
    let cur = "";
    for (const seg of parts) {
      cur += "/" + seg;
      const n = treeNodesByPath.get(cur);
      if (!n || !n.entry || !n.entry.is_dir) continue;
      if (!n.state.loaded) await expandDir(cur, n);
      if (!n.state.expanded) {
        n.state.expanded = true;
        n.state.children.style.display = "block";
        if (n.twisty) n.twisty.textContent = "−";
      }
    }
  }

  // Init restore state
  try {
    const last = localStorage.getItem(LS_KEY_PATH);
    if (last) pathEl.value = last;
    const f = localStorage.getItem(LS_KEY_FILTER);
    if (f && filterEl && !filterEl.value) filterEl.value = f;
    const raw = localStorage.getItem(LS_KEY_EXPANDED);
    if (raw) {
      const arr = JSON.parse(raw);
      if (Array.isArray(arr)) arr.forEach((p) => typeof p === "string" && expandedDirs.add(p));
    }
  } catch (_) {}

  await refreshTree();
  await list();
})();
