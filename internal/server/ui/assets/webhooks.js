(function () {
  const tbody = document.getElementById("hook-table");
  const url = document.getElementById("hook-url");
  const tls = document.getElementById("hook-tls");
  const add = document.getElementById("hook-add");
  const refresh = document.getElementById("hook-refresh");

  function row(h) {
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td class="mono">${escapeHtml(h.URL || "")}</td>
      <td class="mono">${h.CheckTLS ? "on" : "off"}</td>
      <td><button class="btn btn--ghost btn--sm">Delete</button></td>
    `;
    tr.querySelector("button").addEventListener("click", async () => {
      await api({ action: "delete", url: h.URL, check_tls: true });
      await load();
    });
    return tr;
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

  async function api(body) {
    const res = await fetch("/ui/api/webhooks", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(body),
    });
    return res.json().catch(() => null);
  }

  async function load() {
    const res = await fetch("/ui/api/webhooks");
    const data = await res.json().catch(() => null);
    tbody.innerHTML = "";
    if (!data || !data.ok) {
      const tr = document.createElement("tr");
      tr.innerHTML = `<td class="mono" colspan="3">${escapeHtml((data && data.error) || "error")}</td>`;
      tbody.appendChild(tr);
      return;
    }
    (data.webhooks || []).forEach((h) => tbody.appendChild(row(h)));
    if ((data.webhooks || []).length === 0) {
      const tr = document.createElement("tr");
      tr.innerHTML = `<td class="mono" colspan="3">(none)</td>`;
      tbody.appendChild(tr);
    }
  }

  if (add) add.addEventListener("click", async () => {
    const u = (url.value || "").trim();
    if (!u) return;
    const data = await api({ action: "add", url: u, check_tls: !!tls.checked });
    if (!data || !data.ok) alert((data && data.error) || "failed");
    await load();
  });
  if (refresh) refresh.addEventListener("click", load);
  load();
})();

