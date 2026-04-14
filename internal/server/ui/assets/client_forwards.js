(function () {
  const clientIdEl = document.getElementById("client-id");
  const clientFpEl = document.getElementById("client-fp");
  const clientId = clientIdEl ? (clientIdEl.textContent || "").trim() : "";
  const fingerprint = clientFpEl ? (clientFpEl.textContent || "").trim() : "";

  const rfAddr = document.getElementById("rf-addr");
  const rfOn = document.getElementById("rf-on");
  const rfOff = document.getElementById("rf-off");
  const rfList = document.getElementById("rf-list");
  const rfOut = document.getElementById("rf-out");

  async function listForwards() {
    if (!clientId) return;
    const res = await fetch(`/ui/api/forwards/client/${encodeURIComponent(clientId)}`);
    const data = await res.json().catch(() => null);
    if (!data || !data.ok) {
      if (rfOut) rfOut.textContent = (data && data.error) || "error";
      return;
    }
    if (rfOut) rfOut.textContent = (data.forwards || []).join("\n") || "(none)";
  }

  async function setForward(action) {
    const addr = (rfAddr && rfAddr.value || "").trim();
    if (!clientId || !addr) return;
    const res = await fetch(`/ui/api/forwards/client/${encodeURIComponent(clientId)}`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ action, addr }),
    });
    const data = await res.json().catch(() => null);
    if (!data) {
      if (rfOut) rfOut.textContent = "error";
      return;
    }
    if (!data.ok) {
      if (rfOut) rfOut.textContent = (data.errors || []).join("\n") || data.error || "failed";
      return;
    }
    if (rfOut) rfOut.textContent = `Applied: ${data.applied}`;
  }

  if (rfList) rfList.addEventListener("click", listForwards);
  if (rfOn) rfOn.addEventListener("click", () => setForward("on"));
  if (rfOff) rfOff.addEventListener("click", () => setForward("off"));

  const tunPort = document.getElementById("tun-port");
  const tunTarget = document.getElementById("tun-target");
  const tunCreate = document.getElementById("tun-create");
  const tunRefresh = document.getElementById("tun-refresh");
  const tunRows = document.getElementById("tun-rows");
  const tunHint = document.getElementById("tun-hint");

  function esc(str) {
    return String(str).replace(/[&<>"']/g, (c) => ({
      "&": "&amp;",
      "<": "&lt;",
      ">": "&gt;",
      '"': "&quot;",
      "'": "&#39;",
    }[c]));
  }

  async function loadTunnels() {
    if (!tunRows) return;
    const res = await fetch("/ui/api/tunnels");
    const data = await res.json().catch(() => null);
    if (!data || !data.ok) {
      if (tunHint) tunHint.textContent = (data && data.error) || "failed";
      return;
    }
    const tunnels = (data.tunnels || []).filter((t) => (t.fingerprint || "") === fingerprint);
    tunRows.innerHTML = tunnels
      .map((t) => {
        const created = t.created_at ? new Date(t.created_at).toLocaleString() : "";
        return `<tr>
          <td class="mono">${esc(t.id || "")}</td>
          <td class="mono">${esc(t.listen_addr || "")}</td>
          <td class="mono">${esc(t.target || "")}</td>
          <td class="mono">${esc(created)}</td>
          <td><button class="btn btn--ghost btn--sm tun-close" data-id="${esc(t.id || "")}">Close</button></td>
        </tr>`;
      })
      .join("");
    tunRows.querySelectorAll("button.tun-close").forEach((b) => {
      b.addEventListener("click", async () => {
        const id = b.getAttribute("data-id");
        if (!id) return;
        const res = await fetch(`/ui/api/tunnels/${encodeURIComponent(id)}/close`, { method: "POST" });
        const d = await res.json().catch(() => null);
        if (!d || !d.ok) {
          if (tunHint) tunHint.textContent = (d && d.error) || "close failed";
          return;
        }
        await loadTunnels();
      });
    });
    if (tunHint) tunHint.textContent = tunnels.length ? "" : "(none)";
  }

  async function createTunnel() {
    const listen_port = (tunPort && tunPort.value || "").trim();
    const target = (tunTarget && tunTarget.value || "").trim();
    if (!fingerprint || !listen_port || !target) {
      if (tunHint) tunHint.textContent = "port/target required";
      return;
    }
    if (tunHint) tunHint.textContent = "Creating…";
    const res = await fetch("/ui/api/tunnels", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ fingerprint, listen_port, target }),
    });
    const data = await res.json().catch(() => null);
    if (!data || !data.ok) {
      if (tunHint) tunHint.textContent = (data && data.error) || "create failed";
      return;
    }
    if (tunHint) tunHint.textContent = `Listening on ${data.tunnel && data.tunnel.listen_addr ? data.tunnel.listen_addr : "created"}`;
    await loadTunnels();
  }

  if (tunRefresh) tunRefresh.addEventListener("click", loadTunnels);
  if (tunCreate) tunCreate.addEventListener("click", createTunnel);
  loadTunnels();

  const socksBind = document.getElementById("socks-bind");
  const socksExpose = document.getElementById("socks-expose");
  const socksPort = document.getElementById("socks-port");
  const socksCreate = document.getElementById("socks-create");
  const socksRefresh = document.getElementById("socks-refresh");
  const socksRows = document.getElementById("socks-rows");
  const socksHint = document.getElementById("socks-hint");

  function normalizeBind(v) {
    v = (v || "").trim();
    if (!v) return "";
    if (v.startsWith("[") && v.endsWith("]")) return v.slice(1, -1);
    return v;
  }

  if (socksBind && !socksBind.value) socksBind.value = "127.0.0.1";
  if (socksExpose && socksBind) {
    socksExpose.addEventListener("change", () => {
      if (socksExpose.checked) {
        socksBind.value = "0.0.0.0";
      } else if (normalizeBind(socksBind.value) === "0.0.0.0") {
        socksBind.value = "127.0.0.1";
      }
    });
    socksBind.addEventListener("input", () => {
      const v = normalizeBind(socksBind.value);
      socksExpose.checked = v === "0.0.0.0";
    });
    socksExpose.checked = normalizeBind(socksBind.value) === "0.0.0.0";
  }

  async function loadSocks() {
    if (!socksRows) return;
    const res = await fetch("/ui/api/socks");
    const data = await res.json().catch(() => null);
    if (!data || !data.ok) {
      if (socksHint) socksHint.textContent = (data && data.error) || "failed";
      return;
    }
    const proxies = (data.proxies || []).filter((p) => (p.fingerprint || "") === fingerprint);
    socksRows.innerHTML = proxies
      .map((p) => {
        const created = p.created_at ? new Date(p.created_at).toLocaleString() : "";
        return `<tr>
          <td class="mono">${esc(p.id || "")}</td>
          <td class="mono">${esc(p.listen_addr || "")}</td>
          <td class="mono">${esc(created)}</td>
          <td><button class="btn btn--ghost btn--sm socks-close" data-id="${esc(p.id || "")}">Close</button></td>
        </tr>`;
      })
      .join("");
    socksRows.querySelectorAll("button.socks-close").forEach((b) => {
      b.addEventListener("click", async () => {
        const id = b.getAttribute("data-id");
        if (!id) return;
        const res = await fetch(`/ui/api/socks/${encodeURIComponent(id)}/close`, { method: "POST" });
        const d = await res.json().catch(() => null);
        if (!d || !d.ok) {
          if (socksHint) socksHint.textContent = (d && d.error) || "close failed";
          return;
        }
        await loadSocks();
      });
    });
    if (socksHint) socksHint.textContent = proxies.length ? "" : "(none)";
  }

  async function createSocks() {
    const bind_addr = normalizeBind(socksBind && socksBind.value || "");
    const listen_port = (socksPort && socksPort.value || "").trim();
    if (!fingerprint || !listen_port) {
      if (socksHint) socksHint.textContent = "port required";
      return;
    }
    if (socksHint) socksHint.textContent = "Creating…";
    const res = await fetch("/ui/api/socks", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ fingerprint, bind_addr, listen_port }),
    });
    const data = await res.json().catch(() => null);
    if (!data || !data.ok) {
      if (socksHint) socksHint.textContent = (data && data.error) || "create failed";
      return;
    }
    if (socksHint) socksHint.textContent = `Listening on ${data.proxy && data.proxy.listen_addr ? data.proxy.listen_addr : "created"}`;
    await loadSocks();
  }

  if (socksRefresh) socksRefresh.addEventListener("click", loadSocks);
  if (socksCreate) socksCreate.addEventListener("click", createSocks);
  loadSocks();
})();

