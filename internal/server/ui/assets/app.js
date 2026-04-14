(function () {
  if (!window.EventSource) return;

  const totalPill = document.querySelector(".meta__pill");

  function allRows() {
    return Array.from(document.querySelectorAll('tr[data-id]'));
  }

  function setTotal(n) {
    if (!totalPill) return;
    totalPill.textContent = `Total: ${n}`;
  }

  function removeRowById(id) {
    document.querySelectorAll(`tr[data-id="${CSS.escape(id)}"]`).forEach((tr) => tr.remove());
    cleanupEmptyGroups();
    setTotal(allRows().length);
  }

  function cleanupEmptyGroups() {
    document.querySelectorAll("details.group").forEach((d) => {
      const tbody = d.querySelector("tbody");
      if (!tbody) return;
      if (tbody.querySelectorAll("tr[data-id]").length === 0) d.remove();
    });
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
            <th style="width:90px">Status</th>
            <th>ID</th>
            <th>Host</th>
            <th>Remote</th>
            <th>OS</th>
            <th>Owners</th>
            <th>Key</th>
            <th>Version</th>
            <th>Note</th>
          </tr>
        </thead>
        <tbody></tbody>
      </table>
    `;
    details.appendChild(wrap);

    // Insert in sorted order, keeping (ungrouped) last.
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
    return details;
  }

  function updateGroupCount(details) {
    const countEl = details && details.querySelector(".group__count");
    const tbody = details && details.querySelector("tbody");
    if (!countEl || !tbody) return;
    const n = tbody.querySelectorAll("tr[data-id]").length;
    countEl.textContent = String(n);
  }

  function parseOS(version) {
    const m = String(version || "").match(/(?:^|[- ])([a-z0-9]+)_([a-z0-9]+)$/i);
    if (!m) return "";
    return `${m[1].toLowerCase()}/${m[2].toLowerCase()}`;
  }

  async function upsertRow(e) {
    const id = e.id;
    if (!id) return;
    const status = (e.status || "").toLowerCase() || "disconnected";
    const groupName = (e.group || "").trim() || "(ungrouped)";

    // Remove existing row (might be in different group), then insert into correct group.
    removeRowById(id);
    const details = ensureGroup(groupName);
    const tbody = details.querySelector("tbody");
    const tr = document.createElement("tr");
    tr.dataset.id = id;
    tr.dataset.status = status;
    tr.dataset.fp = e.fingerprint || "";
    tr.dataset.group = e.group || "";
    tr.dataset.note = e.note || "";

    const owners = e.owners || "public";
    const keyLabel = e.key_label || (e.comment ? e.comment : (e.fingerprint || ""));
    const os = e.os || parseOS(e.version || "");
    const note = e.note || "";
    const dot = status === "connected"
      ? `<span class="dot dot--online" title="online"></span>`
      : `<span class="dot dot--offline" title="offline"></span>`;
    const delBtn = status === "connected" ? "" : `<button class="btn btn--ghost btn--sm delete-client" type="button" title="Delete offline client"><span class="ico ico--trash" aria-hidden="true"></span></button>`;
    tr.innerHTML = `
      <td><input type="checkbox" class="sel" aria-label="Select client" /></td>
      <td>${dot}<span class="mono">${escapeHtml(status)}</span></td>
      <td><a class="mono" href="/ui/clients/${encodeURIComponent(id)}">${escapeHtml(id)}</a></td>
      <td>${escapeHtml(e.hostname || e.hostName || "")}</td>
      <td class="mono">${escapeHtml(e.remote_addr || e.ip || "")}</td>
      <td class="mono">${escapeHtml(os)}</td>
      <td>${escapeHtml(owners)}</td>
      <td class="mono">${escapeHtml(keyLabel)}</td>
      <td class="mono">${escapeHtml(e.version || "")}</td>
      <td class="note-cell" title="${escapeAttr(note)}">
        <button class="btn btn--ghost btn--sm edit-meta" type="button">Edit</button>
        ${delBtn}
        <span class="note-text">${escapeHtml(note)}</span>
      </td>
    `;
    if (status === "connected") tbody.prepend(tr);
    else tbody.appendChild(tr);
    updateGroupCount(details);
    setTotal(allRows().length);

    const table = details.querySelector("table.clients-table");
    if (table && window.rsshSortTable) window.rsshSortTable(table);

    if (window.rsshClients && typeof window.rsshClients.wireNewTable === "function") {
      window.rsshClients.wireNewTable(table);
    }
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

  function escapeAttr(str) {
    return escapeHtml(str).replace(/"/g, "&quot;");
  }

  const es = new EventSource("/ui/events");
  es.addEventListener("client", (ev) => {
    try {
      const msg = JSON.parse(ev.data);
      upsertRow(msg);
    } catch (_) {}
  });
})();
