(function () {
  const form = document.getElementById("exec-form");
  const out = document.getElementById("out");
  const clear = document.getElementById("clear-output");
  const kill = document.getElementById("kill-btn");
  const owners = document.getElementById("owners");
  const ownersSave = document.getElementById("owners-save");
  const group = document.getElementById("group");
  const groupSave = document.getElementById("group-save");
  const note = document.getElementById("note");
  const noteSave = document.getElementById("note-save");
  const commSave = document.getElementById("comm-save");
  const commHint = document.getElementById("comm-hint");
  const commServerTimeout = document.getElementById("comm-server-timeout");
  const commClientHeartbeat = document.getElementById("comm-client-heartbeat");
  const commSleepWindow = document.getElementById("comm-sleep-window");
  const commSleepCheck = document.getElementById("comm-sleep-check");
  const commSleepUntil = document.getElementById("comm-sleep-until");

  const sleepOpen = document.getElementById("sleep-open");
  const sleepClear = document.getElementById("sleep-clear");
  const sleepHint = document.getElementById("sleep-hint");
  const sleepModal = document.getElementById("sleep-modal");
  const sleepBackdrop = document.getElementById("sleep-backdrop");
  const sleepTitle = document.getElementById("sleep-title");
  const sleepSave = document.getElementById("sleep-save");
  const sleepClose = document.getElementById("sleep-close");
  const sleepMode = document.getElementById("sleep-mode");
  const sleepNow = document.getElementById("sleep-now");
  const sleepUntil = document.getElementById("sleep-until");
  const sleepMinutes = document.getElementById("sleep-minutes");
  const sleepDate = document.getElementById("sleep-date");
  const sleepTime = document.getElementById("sleep-time");
  const sleepModalHint = document.getElementById("sleep-modal-hint");

  function showSleepModal() {
    if (!sleepModal) return;
    sleepModal.style.display = "block";
    sleepModal.setAttribute("aria-hidden", "false");
    document.body.style.overflow = "hidden";
  }

  function hideSleepModal() {
    if (!sleepModal) return;
    sleepModal.style.display = "none";
    sleepModal.setAttribute("aria-hidden", "true");
    document.body.style.overflow = "";
  }

  function pad2(n) {
    return String(n).padStart(2, "0");
  }

  function toRFC3339Local(d) {
    const off = -d.getTimezoneOffset(); // minutes east of UTC
    const sign = off >= 0 ? "+" : "-";
    const abs = Math.abs(off);
    const oh = pad2(Math.floor(abs / 60));
    const om = pad2(abs % 60);
    return (
      d.getFullYear() +
      "-" +
      pad2(d.getMonth() + 1) +
      "-" +
      pad2(d.getDate()) +
      "T" +
      pad2(d.getHours()) +
      ":" +
      pad2(d.getMinutes()) +
      ":" +
      pad2(d.getSeconds()) +
      sign +
      oh +
      ":" +
      om
    );
  }

  if (clear && out) clear.addEventListener("click", () => (out.value = ""));

  function setStatus(text) {
    if (!out) return;
    out.value = text;
  }

  if (form) {
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      const id = form.querySelector('input[name="id"]').value;
      const cmd = form.querySelector('input[name="cmd"]').value.trim();
      if (!cmd) return;
      setStatus(`$ ${cmd}\n\n(running...)`);

      const res = await fetch(`/ui/api/clients/${encodeURIComponent(id)}/exec`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ cmd }),
      });
      const data = await res.json().catch(() => null);
      if (!data) {
        setStatus("Error: invalid response");
        return;
      }
      if (!data.ok) {
        setStatus(`$ ${cmd}\n\nERROR: ${data.error || "unknown"}\n\n${data.stdout || ""}`);
        return;
      }
      setStatus(`$ ${cmd}\n\n${data.stdout || ""}`);
    });
  }

  if (kill) {
    kill.addEventListener("click", async () => {
      const id = kill.dataset.id;
      if (!id) return;
      if (!confirm(`Kill client ${id}?`)) return;
      kill.disabled = true;
      const res = await fetch(`/ui/api/clients/${encodeURIComponent(id)}/kill`, { method: "POST" });
      const data = await res.json().catch(() => null);
      if (data && data.ok) {
        window.location.href = "/ui/";
      } else {
        alert("Kill failed");
        kill.disabled = false;
      }
    });
  }

  if (ownersSave && owners) {
    ownersSave.addEventListener("click", async () => {
      const id = ownersSave.dataset.id;
      const value = owners.value.trim();
      ownersSave.disabled = true;
      const res = await fetch(`/ui/api/clients/${encodeURIComponent(id)}/owners`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ owners: value }),
      });
      const data = await res.json().catch(() => null);
      if (!data || !data.ok) {
        alert((data && data.error) || "save failed");
      }
      ownersSave.disabled = false;
    });
  }

  async function saveMeta(id) {
    if (!id) return;
    const g = (group && group.value) || "";
    const n = (note && note.value) || "";
    const res = await fetch(`/ui/api/clients/${encodeURIComponent(id)}/meta`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ group: g, note: n }),
    });
    const data = await res.json().catch(() => null);
    if (!data || !data.ok) {
      alert((data && data.error) || "save failed");
      return false;
    }
    return true;
  }

  if (groupSave && group) {
    groupSave.addEventListener("click", async () => {
      const id = groupSave.dataset.id;
      groupSave.disabled = true;
      await saveMeta(id);
      groupSave.disabled = false;
    });
  }

  if (noteSave && note) {
    noteSave.addEventListener("click", async () => {
      const id = noteSave.dataset.id;
      noteSave.disabled = true;
      await saveMeta(id);
      noteSave.disabled = false;
    });
  }

  if (commSave) {
    commSave.addEventListener("click", async () => {
      const id = commSave.dataset.id;
      if (!id) return;
      const payload = {
        server_timeout_seconds: parseInt((commServerTimeout && commServerTimeout.value) || "0", 10) || 0,
        client_heartbeat_sec: parseInt((commClientHeartbeat && commClientHeartbeat.value) || "0", 10) || 0,
        sleep_window: ((commSleepWindow && commSleepWindow.value) || "").trim(),
        sleep_check_sec: parseInt((commSleepCheck && commSleepCheck.value) || "0", 10) || 0,
        sleep_until: ((commSleepUntil && commSleepUntil.value) || "").trim(),
      };
      commSave.disabled = true;
      if (commHint) commHint.textContent = "Saving…";
      const res = await fetch(`/ui/api/clients/${encodeURIComponent(id)}/comm`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(payload),
      });
      const data = await res.json().catch(() => null);
      if (!data || !data.ok) {
        if (commHint) commHint.textContent = (data && data.error) || "save failed";
        commSave.disabled = false;
        return;
      }
      if (commHint) commHint.textContent = "Saved.";
      commSave.disabled = false;
    });
  }

  function updateSleepModeUI() {
    const mode = (sleepMode && sleepMode.value) || "now";
    if (sleepNow) sleepNow.style.display = mode === "now" ? "block" : "none";
    if (sleepUntil) sleepUntil.style.display = mode === "until" ? "block" : "none";
  }

  async function applySleep(id, body) {
    const res = await fetch(`/ui/api/clients/${encodeURIComponent(id)}/sleep`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(body),
    });
    const data = await res.json().catch(() => null);
    if (!data || !data.ok) throw new Error((data && data.error) || "sleep failed");
    return data;
  }

  if (sleepMode) sleepMode.addEventListener("change", updateSleepModeUI);

  if (sleepOpen) {
    sleepOpen.addEventListener("click", () => {
      const id = sleepOpen.dataset.id;
      if (sleepTitle) sleepTitle.textContent = id || "";
      if (sleepModalHint) sleepModalHint.textContent = "";
      if (sleepMode) sleepMode.value = "now";
      if (sleepMinutes) sleepMinutes.value = "60";
      if (sleepDate) {
        const d = new Date();
        sleepDate.value = `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`;
      }
      if (sleepTime) sleepTime.value = "00:00";
      updateSleepModeUI();
      showSleepModal();
    });
  }

  if (sleepClear) {
    sleepClear.addEventListener("click", async () => {
      const id = sleepClear.dataset.id;
      if (!id) return;
      sleepClear.disabled = true;
      if (sleepHint) sleepHint.textContent = "Clearing…";
      try {
        const d = await applySleep(id, { mode: "clear" });
        if (commSleepUntil) commSleepUntil.value = d.sleep_until || "";
        if (sleepHint) sleepHint.textContent = "Cleared.";
      } catch (e) {
        if (sleepHint) sleepHint.textContent = e && e.message ? e.message : "failed";
      } finally {
        sleepClear.disabled = false;
      }
    });
  }

  if (sleepSave) {
    sleepSave.addEventListener("click", async () => {
      const id = (sleepOpen && sleepOpen.dataset && sleepOpen.dataset.id) || (sleepClear && sleepClear.dataset && sleepClear.dataset.id) || "";
      const mode = (sleepMode && sleepMode.value) || "now";
      sleepSave.disabled = true;
      if (sleepModalHint) sleepModalHint.textContent = "Applying…";
      try {
        let body = null;
        if (mode === "now") {
          const mins = parseInt((sleepMinutes && sleepMinutes.value) || "0", 10) || 0;
          body = { mode: "now", minutes: mins };
        } else {
          const d = (sleepDate && sleepDate.value) || "";
          const t = (sleepTime && sleepTime.value) || "00:00";
          if (!d) throw new Error("date is empty");
          const parts = d.split("-").map((x) => parseInt(x, 10));
          const tparts = t.split(":").map((x) => parseInt(x, 10));
          const dt = new Date(parts[0], parts[1] - 1, parts[2], tparts[0] || 0, tparts[1] || 0, 0, 0);
          body = { mode: "until", until: toRFC3339Local(dt) };
        }
        const out = await applySleep(id, body);
        if (commSleepUntil) commSleepUntil.value = out.sleep_until || "";
        if (sleepModalHint) sleepModalHint.textContent = "Applied. Client will disconnect for sleep.";
        setTimeout(() => window.location.reload(), 1200);
      } catch (e) {
        if (sleepModalHint) sleepModalHint.textContent = e && e.message ? e.message : "failed";
      } finally {
        sleepSave.disabled = false;
      }
    });
  }

  if (sleepClose) sleepClose.addEventListener("click", hideSleepModal);
  if (sleepBackdrop) sleepBackdrop.addEventListener("click", hideSleepModal);

  // Networking summary (remote forwards / tunnels / SOCKS) for the client page.
  const netRemoteCount = document.getElementById("net-remote-count");
  const netTunnelCount = document.getElementById("net-tunnel-count");
  const netSocksCount = document.getElementById("net-socks-count");
  const fpEl = document.getElementById("client-fp");
  const fingerprint = fpEl ? (fpEl.textContent || "").trim() : "";
  const netTunPort = document.getElementById("net-tun-port");
  const netTunTarget = document.getElementById("net-tun-target");
  const netTunCreate = document.getElementById("net-tun-create");
  const netTunHint = document.getElementById("net-tun-hint");
  const netSocksBind = document.getElementById("net-socks-bind");
  const netSocksPort = document.getElementById("net-socks-port");
  const netSocksCreate = document.getElementById("net-socks-create");
  const netSocksHint = document.getElementById("net-socks-hint");
  const netSocksExpose = document.getElementById("net-socks-expose");

  async function loadNetSummary() {
    const id =
      (kill && kill.dataset && kill.dataset.id) ||
      (ownersSave && ownersSave.dataset && ownersSave.dataset.id) ||
      (groupSave && groupSave.dataset && groupSave.dataset.id) ||
      "";
    if (!id || !fingerprint) return;

    try {
      const res = await fetch(`/ui/api/forwards/client/${encodeURIComponent(id)}`);
      const data = await res.json().catch(() => null);
      if (data && data.ok) {
        const n = (data.forwards || []).length;
        if (netRemoteCount) netRemoteCount.textContent = String(n);
      } else {
        if (netRemoteCount) netRemoteCount.textContent = "—";
      }
    } catch (_) {
      if (netRemoteCount) netRemoteCount.textContent = "—";
    }

    try {
      const res = await fetch("/ui/api/tunnels");
      const data = await res.json().catch(() => null);
      if (data && data.ok) {
        const n = (data.tunnels || []).filter((t) => (t.fingerprint || "") === fingerprint).length;
        if (netTunnelCount) netTunnelCount.textContent = String(n);
      }
    } catch (_) {}

    try {
      const res = await fetch("/ui/api/socks");
      const data = await res.json().catch(() => null);
      if (data && data.ok) {
        const n = (data.proxies || []).filter((p) => (p.fingerprint || "") === fingerprint).length;
        if (netSocksCount) netSocksCount.textContent = String(n);
      }
    } catch (_) {}
  }

  if (netRemoteCount || netTunnelCount || netSocksCount) {
    loadNetSummary();
  }

  function normalizeBind(v) {
    v = (v || "").trim();
    if (!v) return "";
    if (v.startsWith("[") && v.endsWith("]")) return v.slice(1, -1);
    return v;
  }

  const netKeyBase = `rssh:clientnet:${fingerprint}:`;
  try {
    if (netTunPort && !netTunPort.value) netTunPort.value = localStorage.getItem(netKeyBase + "tun_port") || "";
    if (netTunTarget && !netTunTarget.value) netTunTarget.value = localStorage.getItem(netKeyBase + "tun_target") || "";
    if (netSocksBind && !netSocksBind.value) netSocksBind.value = localStorage.getItem(netKeyBase + "socks_bind") || "127.0.0.1";
    if (netSocksPort && !netSocksPort.value) netSocksPort.value = localStorage.getItem(netKeyBase + "socks_port") || "";
  } catch (_) {}

  function saveNetState() {
    try {
      if (netTunPort) localStorage.setItem(netKeyBase + "tun_port", netTunPort.value || "");
      if (netTunTarget) localStorage.setItem(netKeyBase + "tun_target", netTunTarget.value || "");
      if (netSocksBind) localStorage.setItem(netKeyBase + "socks_bind", netSocksBind.value || "");
      if (netSocksPort) localStorage.setItem(netKeyBase + "socks_port", netSocksPort.value || "");
    } catch (_) {}
  }

  if (netTunPort) netTunPort.addEventListener("input", saveNetState);
  if (netTunTarget) netTunTarget.addEventListener("input", saveNetState);
  if (netSocksBind) netSocksBind.addEventListener("input", () => {
    saveNetState();
    if (netSocksExpose) netSocksExpose.checked = normalizeBind(netSocksBind.value) === "0.0.0.0";
  });
  if (netSocksPort) netSocksPort.addEventListener("input", saveNetState);
  if (netSocksExpose && netSocksBind) {
    netSocksExpose.checked = normalizeBind(netSocksBind.value) === "0.0.0.0";
    netSocksExpose.addEventListener("change", () => {
      if (netSocksExpose.checked) {
        netSocksBind.value = "0.0.0.0";
      } else if (normalizeBind(netSocksBind.value) === "0.0.0.0") {
        netSocksBind.value = "127.0.0.1";
      }
      saveNetState();
    });
  }

  if (netTunCreate) {
    netTunCreate.addEventListener("click", async () => {
      const listen_port = (netTunPort && netTunPort.value || "").trim();
      const target = (netTunTarget && netTunTarget.value || "").trim();
      if (!fingerprint || !listen_port || !target) {
        if (netTunHint) netTunHint.textContent = "port/target required";
        return;
      }
      if (netTunHint) netTunHint.textContent = "Creating…";
      const res = await fetch("/ui/api/tunnels", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ fingerprint, listen_port, target }),
      });
      const data = await res.json().catch(() => null);
      if (!data || !data.ok) {
        if (netTunHint) netTunHint.textContent = (data && data.error) || "create failed";
        return;
      }
      if (netTunHint) netTunHint.textContent = `Listening on ${data.tunnel && data.tunnel.listen_addr ? data.tunnel.listen_addr : "created"}`;
      loadNetSummary();
    });
  }

  if (netSocksCreate) {
    netSocksCreate.addEventListener("click", async () => {
      const bind_addr = normalizeBind(netSocksBind && netSocksBind.value || "");
      const listen_port = (netSocksPort && netSocksPort.value || "").trim();
      if (!fingerprint || !listen_port) {
        if (netSocksHint) netSocksHint.textContent = "port required";
        return;
      }
      if (netSocksHint) netSocksHint.textContent = "Creating…";
      const res = await fetch("/ui/api/socks", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ fingerprint, bind_addr, listen_port }),
      });
      const data = await res.json().catch(() => null);
      if (!data || !data.ok) {
        if (netSocksHint) netSocksHint.textContent = (data && data.error) || "create failed";
        return;
      }
      if (netSocksHint) netSocksHint.textContent = `Listening on ${data.proxy && data.proxy.listen_addr ? data.proxy.listen_addr : "created"}`;
      loadNetSummary();
    });
  }
})();
