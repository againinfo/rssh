(function () {
  function qs(sel, root) {
    return (root || document).querySelector(sel);
  }
  function qsa(sel, root) {
    return Array.from((root || document).querySelectorAll(sel));
  }

  function escapeHtml(str) {
    return String(str).replace(/[&<>"']/g, (c) => ({
      "&": "&amp;",
      "<": "&lt;",
      ">": "&gt;",
      '"': "&quot;",
      "'": "&#39;",
    }[c]));
  }

  const expandAll = qs("#expand-all");
  const collapseAll = qs("#collapse-all");
  const bulkGroup = qs("#bulk-group");
  const bulkNote = qs("#bulk-note");

  const modal = qs("#client-meta-modal");
  const backdrop = qs("#client-meta-backdrop");
  const titleEl = qs("#client-meta-title");
  const groupEl = qs("#client-meta-group");
  const noteEl = qs("#client-meta-note");
  const hintEl = qs("#client-meta-hint");
  const saveBtn = qs("#client-meta-save");
  const closeBtn = qs("#client-meta-close");

  const bulkFlags = qs("#client-meta-bulk-flags");
  const applyGroupEl = qs("#client-meta-apply-group");
  const applyNoteEl = qs("#client-meta-apply-note");

  let editingRow = null;
  let bulkFingerprints = null;

  function setAllDetails(open) {
    qsa("details.group").forEach((d) => (d.open = !!open));
  }

  function selectedFingerprints() {
    const fps = [];
    qsa("input.sel:checked").forEach((cb) => {
      const tr = cb.closest("tr");
      const fp = tr && tr.dataset ? tr.dataset.fp : "";
      if (fp) fps.push(fp);
    });
    return fps;
  }

  async function postBulkMeta(payload) {
    const res = await fetch("/ui/api/clientmeta", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(payload),
    });
    const data = await res.json().catch(() => null);
    if (!data || !data.ok) throw new Error((data && data.error) || "request failed");
    return data;
  }

  function wireGroupSelectAll() {
    qsa("table.clients-table").forEach((table) => {
      const all = qs("input.sel-all", table);
      if (!all) return;
      all.addEventListener("change", () => {
        const checked = !!all.checked;
        qsa("input.sel", table).forEach((cb) => (cb.checked = checked));
      });
    });
  }

  function wireSelectAllInTable(table) {
    if (!table) return;
    const all = qs("input.sel-all", table);
    if (!all || all.dataset.wired === "1") return;
    all.dataset.wired = "1";
    all.addEventListener("change", () => {
      const checked = !!all.checked;
      qsa("input.sel", table).forEach((cb) => (cb.checked = checked));
    });
  }

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
    editingRow = null;
    bulkFingerprints = null;
  }

  function updateGroupCount(details) {
    const countEl = details && qs(".group__count", details);
    const tbody = details && qs("tbody", details);
    if (!countEl || !tbody) return;
    countEl.textContent = String(tbody.querySelectorAll("tr[data-id]").length);
  }

  function ensureGroup(name) {
    const key = name && name.trim() ? name.trim() : "(ungrouped)";
    let details = document.querySelector(`details.group[data-group="${CSS.escape(key)}"]`);
    if (details) return details;

    details = document.createElement("details");
    details.className = "group";
    details.dataset.group = key;
    details.open = true;

    const summary = document.createElement("summary");
    summary.className = "group__summary";
    summary.innerHTML = `<span class="group__title">${escapeHtml(key)}</span><span class="group__count">0</span>`;
    details.appendChild(summary);

    const wrap = document.createElement("div");
    wrap.className = "tablewrap";
    wrap.style.marginTop = "10px";
    wrap.innerHTML = `
      <table class="table clients-table" data-group-table="${escapeHtml(key)}">
        <thead>
          <tr>
            <th style="width:42px"><input type="checkbox" class="sel-all" aria-label="Select all in group" /></th>
            <th data-col="status" style="width:90px">Status</th>
            <th data-col="id">ID</th>
            <th data-col="host">Host</th>
            <th data-col="remote">Remote</th>
            <th data-col="os">OS</th>
            <th data-col="owners">Owners</th>
            <th data-col="key">Key</th>
            <th data-col="version">Version</th>
            <th data-col="note">Note</th>
          </tr>
        </thead>
        <tbody></tbody>
      </table>
    `;
    details.appendChild(wrap);

    const groups = Array.from(document.querySelectorAll("details.group"));
    const container = groups.length ? groups[0].parentElement : document.querySelector("section.card");
    if (!container) return details;

    const isUng = key === "(ungrouped)";
    let inserted = false;
    for (const g of groups) {
      const gn = (g.dataset.group || "").trim();
      if (gn === "(ungrouped)") {
        if (!isUng) {
          container.insertBefore(details, g);
          inserted = true;
          break;
        }
        continue;
      }
      if (isUng) continue;
      if (gn.toLowerCase() > key.toLowerCase()) {
        container.insertBefore(details, g);
        inserted = true;
        break;
      }
    }
    if (!inserted) container.appendChild(details);

    wireGroupSelectAll();
    wireSorting(details);
    return details;
  }

  function currentGroupNameFromDetails(details) {
    return (details && details.dataset && details.dataset.group) || "(ungrouped)";
  }

  function getRowValues(tr) {
    const tds = Array.from(tr.querySelectorAll("td"));
    // columns: 0 sel, 1 status, 2 id, 3 host, 4 remote, 5 os, 6 owners, 7 key, 8 version, 9 note
    const v = (idx) => (tds[idx] ? tds[idx].textContent.trim() : "");
    return {
      status: v(1),
      id: v(2),
      host: v(3),
      remote: v(4),
      os: v(5),
      owners: v(6),
      key: v(7),
      version: v(8),
      note: (tr.dataset && tr.dataset.note) || v(9),
    };
  }

  function sortTable(table, key, dir) {
    const tbody = qs("tbody", table);
    if (!tbody) return;
    const rows = Array.from(tbody.querySelectorAll("tr[data-id]"));
    rows.sort((a, b) => {
      const av = getRowValues(a)[key] || "";
      const bv = getRowValues(b)[key] || "";
      const aa = av.toLowerCase();
      const bb = bv.toLowerCase();
      if (aa === bb) return 0;
      return aa < bb ? -1 : 1;
    });
    if (dir === "desc") rows.reverse();
    rows.forEach((r) => tbody.appendChild(r));
  }

  function updateSortIndicators(table) {
    const key = table.dataset.sortKey || "";
    const dir = table.dataset.sortDir || "asc";
    qsa("th[data-col]", table).forEach((th) => {
      th.classList.remove("sort--asc", "sort--desc");
      if (th.dataset.col === key) th.classList.add(dir === "desc" ? "sort--desc" : "sort--asc");
    });
  }

  function wireSorting(root) {
    qsa("table.clients-table", root || document).forEach((table) => {
      qsa("th[data-col]", table).forEach((th) => {
        th.addEventListener("click", () => {
          const key = th.dataset.col;
          const curKey = table.dataset.sortKey || "";
          const curDir = table.dataset.sortDir || "asc";
          let nextDir = "asc";
          if (curKey === key) nextDir = curDir === "asc" ? "desc" : "asc";
          table.dataset.sortKey = key;
          table.dataset.sortDir = nextDir;
          sortTable(table, key, nextDir);
          updateSortIndicators(table);
        });
      });
    });
  }

  window.rsshSortTable = function (table) {
    if (!table) return;
    const key = table.dataset.sortKey;
    if (!key) return;
    sortTable(table, key, table.dataset.sortDir || "asc");
    updateSortIndicators(table);
  };

  window.rsshClients = {
    wireNewTable: function (table) {
      wireSelectAllInTable(table);
      wireSorting(table);
    },
  };

  function setBulkMode(on) {
    if (bulkFlags) bulkFlags.style.display = on ? "flex" : "none";
  }

  async function openMetaEditor(tr) {
    editingRow = tr;
    bulkFingerprints = null;
    setBulkMode(false);
    if (!editingRow) return;
    const id = editingRow.dataset.id;
    if (titleEl) titleEl.textContent = id;
    if (hintEl) hintEl.textContent = "Loading…";
    if (groupEl) groupEl.value = "";
    if (noteEl) noteEl.value = "";
    showModal();

    const res = await fetch(`/ui/api/clients/${encodeURIComponent(id)}/meta`);
    const data = await res.json().catch(() => null);
    if (!data || !data.ok) {
      if (hintEl) hintEl.textContent = (data && data.error) || "load failed";
      return;
    }
    const meta = data.meta || {};
    if (groupEl) groupEl.value = meta.group || "";
    if (noteEl) noteEl.value = meta.note || "";
    if (hintEl) hintEl.textContent = "";
  }

  async function openBulkEditor(fps, defaults) {
    editingRow = null;
    bulkFingerprints = fps;
    setBulkMode(true);
    if (titleEl) titleEl.textContent = `Selected: ${fps.length}`;
    if (hintEl) hintEl.textContent = "Fill fields and click Save. Unchecked fields won't be changed.";
    if (groupEl) groupEl.value = (defaults && defaults.group) || "";
    if (noteEl) noteEl.value = (defaults && defaults.note) || "";
    if (applyGroupEl) applyGroupEl.checked = defaults && typeof defaults.applyGroup === "boolean" ? defaults.applyGroup : true;
    if (applyNoteEl) applyNoteEl.checked = defaults && typeof defaults.applyNote === "boolean" ? defaults.applyNote : true;
    showModal();
  }

  async function saveMetaEditor() {
    if (!editingRow) return;
    const id = editingRow.dataset.id;
    const oldDetails = editingRow.closest("details.group");
    const oldGroup = currentGroupNameFromDetails(oldDetails);
    const newGroup = (groupEl && groupEl.value ? groupEl.value : "").trim();
    const newNote = (noteEl && noteEl.value ? noteEl.value : "").trim();

    if (saveBtn) saveBtn.disabled = true;
    if (hintEl) hintEl.textContent = "Saving…";
    const res = await fetch(`/ui/api/clients/${encodeURIComponent(id)}/meta`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ group: newGroup, note: newNote }),
    });
    const data = await res.json().catch(() => null);
    if (!data || !data.ok) {
      if (hintEl) hintEl.textContent = (data && data.error) || "save failed";
      if (saveBtn) saveBtn.disabled = false;
      return;
    }

    // Update row note + dataset
    editingRow.dataset.group = newGroup;
    editingRow.dataset.note = newNote;
    const noteText = qs(".note-text", editingRow);
    const noteCell = qs(".note-cell", editingRow);
    if (noteText) noteText.textContent = newNote;
    if (noteCell) noteCell.title = newNote;

    const targetGroupName = newGroup || "(ungrouped)";
    if (oldGroup !== targetGroupName) {
      const newDetails = ensureGroup(targetGroupName);
      const tbody = qs("tbody", newDetails);
      if (tbody) tbody.prepend(editingRow);
      updateGroupCount(newDetails);
      if (oldDetails) updateGroupCount(oldDetails);
      // Remove empty old group (except ungrouped)
      if (oldDetails && qs("tbody", oldDetails) && oldGroup !== "(ungrouped)" && qs("tbody", oldDetails).querySelectorAll("tr[data-id]").length === 0) {
        oldDetails.remove();
      }

      // Re-apply current sort on the target table
      const table = qs("table.clients-table", newDetails);
      if (table && window.rsshSortTable) window.rsshSortTable(table);
    } else {
      const table = oldDetails && qs("table.clients-table", oldDetails);
      if (table && window.rsshSortTable) window.rsshSortTable(table);
      if (oldDetails) updateGroupCount(oldDetails);
    }

    if (hintEl) hintEl.textContent = "Saved.";
    if (saveBtn) saveBtn.disabled = false;
    setTimeout(hideModal, 200);
  }

  async function saveBulkEditor() {
    if (!bulkFingerprints || bulkFingerprints.length === 0) return;
    const applyGroup = !!(applyGroupEl && applyGroupEl.checked);
    const applyNote = !!(applyNoteEl && applyNoteEl.checked);
    if (!applyGroup && !applyNote) {
      alert("Select at least one field to apply");
      return;
    }
    const payload = { fingerprints: bulkFingerprints };
    if (applyGroup) payload.group = (groupEl && groupEl.value ? groupEl.value : "").trim();
    if (applyNote) payload.note = (noteEl && noteEl.value ? noteEl.value : "").trim();

    if (saveBtn) saveBtn.disabled = true;
    if (hintEl) hintEl.textContent = "Saving…";
    try {
      await postBulkMeta(payload);
      window.location.reload();
    } catch (e) {
      if (hintEl) hintEl.textContent = e && e.message ? e.message : "failed";
      if (saveBtn) saveBtn.disabled = false;
    }
  }

  function wireInlineEditors() {
    // Event delegation so SSE-inserted rows also work.
    document.addEventListener("click", (ev) => {
      const btn = ev.target && ev.target.closest ? ev.target.closest("button.edit-meta") : null;
      if (!btn) return;
      ev.preventDefault();
      const tr = btn.closest("tr");
      openMetaEditor(tr);
    });
    document.addEventListener("click", async (ev) => {
      const btn = ev.target && ev.target.closest ? ev.target.closest("button.delete-client") : null;
      if (!btn) return;
      ev.preventDefault();
      const tr = btn.closest("tr");
      const id = tr && tr.dataset ? (tr.dataset.id || "") : "";
      const status = tr && tr.dataset ? (tr.dataset.status || "") : "";
      if (!id) return;
      if (String(status).toLowerCase() === "connected") {
        return alert("Client is online; disconnect it first.");
      }
      if (!confirm(`Delete offline client ${id}? This also removes its saved meta/settings.`)) return;

      btn.disabled = true;
      try {
        const res = await fetch(`/ui/api/clients/${encodeURIComponent(id)}/delete`, { method: "POST" });
        const data = await res.json().catch(() => null);
        if (!data || !data.ok) {
          alert((data && data.error) || "delete failed");
          btn.disabled = false;
          return;
        }
        // Remove row and cleanup group counts.
        const details = tr.closest("details.group");
        tr.remove();
        if (details) {
          updateGroupCount(details);
          const tbody = qs("tbody", details);
          if (tbody && tbody.querySelectorAll("tr[data-id]").length === 0) details.remove();
        }
      } catch (e) {
        alert("delete failed");
        btn.disabled = false;
      }
    });
    document.addEventListener("dblclick", (ev) => {
      const cell = ev.target && ev.target.closest ? ev.target.closest("td.note-cell") : null;
      if (!cell) return;
      const tr = cell.closest("tr");
      openMetaEditor(tr);
    });
  }

  if (expandAll) expandAll.addEventListener("click", () => setAllDetails(true));
  if (collapseAll) collapseAll.addEventListener("click", () => setAllDetails(false));

  if (bulkGroup)
    bulkGroup.addEventListener("click", async () => {
      const fps = selectedFingerprints();
      if (fps.length === 0) return alert("No clients selected");
      openBulkEditor(fps, { applyGroup: true, applyNote: false, group: "", note: "" });
    });

  if (bulkNote)
    bulkNote.addEventListener("click", async () => {
      const fps = selectedFingerprints();
      if (fps.length === 0) return alert("No clients selected");
      openBulkEditor(fps, { applyGroup: false, applyNote: true, group: "", note: "" });
    });

  wireGroupSelectAll();
  wireSorting();
  wireInlineEditors();

  if (closeBtn) closeBtn.addEventListener("click", hideModal);
  if (backdrop) backdrop.addEventListener("click", hideModal);
  if (saveBtn)
    saveBtn.addEventListener("click", () => {
      if (bulkFingerprints) return saveBulkEditor();
      return saveMetaEditor();
    });
  window.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && modal && modal.style.display !== "none") hideModal();
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s" && modal && modal.style.display !== "none") {
      e.preventDefault();
      if (bulkFingerprints) return saveBulkEditor();
      return saveMetaEditor();
    }
  });
})();
