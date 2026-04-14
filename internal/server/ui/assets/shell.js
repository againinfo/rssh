(function () {
  const meta = document.getElementById("client-meta");
  const id = (meta && meta.dataset && meta.dataset.clientId) || "";
  const termHost = document.getElementById("term");
  const connectBtn = document.getElementById("connect");
  const clearBtn = document.getElementById("clear");
  const status = document.getElementById("shell-status");

  let ws = null;
  let term = null;
  let fit = null;

  function setStatus(t) {
    if (status) status.textContent = t || "";
  }

  function wsUrl() {
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    return `${proto}//${location.host}/ui/ws/clients/${encodeURIComponent(id)}/shell`;
  }

  function b64(bytes) {
    let bin = "";
    for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
    return btoa(bin);
  }

  function fromB64(s) {
    const bin = atob(s);
    const out = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
    return out;
  }

  function send(obj) {
    if (!ws || ws.readyState !== 1) return;
    ws.send(JSON.stringify(obj));
  }

  function sendInput(data) {
    const bytes = new TextEncoder().encode(data);
    send({ type: "input", data: b64(bytes) });
  }

  function resize() {
    if (!term || !fit) return;
    try {
      fit.fit();
    } catch (_) {}
    send({ type: "resize", cols: term.cols, rows: term.rows });
  }

  function ensureTerminal() {
    if (term) return;
    if (!window.Terminal) {
      setStatus("xterm.js failed to load");
      return;
    }

    term = new window.Terminal({
      cursorBlink: true,
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace",
      fontSize: 13,
      theme: {
        background: "#ffffff",
        foreground: "#0f172a",
        cursor: "#0ea5e9",
        selectionBackground: "rgba(14,165,233,.22)",
      },
      allowProposedApi: true,
    });

    fit = new window.FitAddon.FitAddon();
    term.loadAddon(fit);
    term.open(termHost);
    resize();

    term.onData((d) => sendInput(d));
    term.onPaste((d) => sendInput(d));

    window.addEventListener("resize", () => resize());
  }

  function connect() {
    ensureTerminal();
    if (!term) return;

    if (ws && (ws.readyState === 0 || ws.readyState === 1)) return;

    setStatus("Connecting…");
    ws = new WebSocket(wsUrl());

    ws.onopen = () => {
      setStatus("Connected");
      resize();
      term.focus();
    };

    ws.onclose = () => setStatus("Closed");
    ws.onerror = () => setStatus("Error");

    ws.onmessage = (ev) => {
      let msg = null;
      try {
        msg = JSON.parse(ev.data);
      } catch (_) {
        return;
      }
      if (!msg || !term) return;

      if (msg.type === "output") {
        const bytes = fromB64(msg.data || "");
        const text = new TextDecoder().decode(bytes);
        term.write(text);
        return;
      }
      if (msg.type === "exit") {
        setStatus("Exited");
        return;
      }
      if (msg.type === "error") {
        setStatus(msg.error || "Error");
      }
    };
  }

  if (connectBtn) connectBtn.addEventListener("click", connect);
  if (clearBtn)
    clearBtn.addEventListener("click", () => {
      if (!term) return;
      term.clear();
      term.reset();
      term.focus();
    });
})();

