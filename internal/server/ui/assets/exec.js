(function () {
  const form = document.getElementById("exec-form");
  const cmd = document.getElementById("cmd");
  const out = document.getElementById("out");
  const clearBtn = document.getElementById("clear-output");
  const copyBtn = document.getElementById("copy-output");
  const hint = document.getElementById("exec-hint");

  if (!form || !cmd || !out) return;

  const clientId = (form.querySelector("input[name=id]") || {}).value || "";
  const keyBase = `rssh:exec:${clientId}:`;

  function loadState() {
    try {
      const lastCmd = localStorage.getItem(keyBase + "cmd");
      const lastOut = localStorage.getItem(keyBase + "out");
      if (lastCmd && !cmd.value) cmd.value = lastCmd;
      if (lastOut && !out.value) out.value = lastOut;
    } catch (_) {}
  }

  function saveState() {
    try {
      localStorage.setItem(keyBase + "cmd", cmd.value || "");
      localStorage.setItem(keyBase + "out", out.value || "");
    } catch (_) {}
  }

  loadState();
  cmd.addEventListener("input", saveState);
  out.addEventListener("input", saveState);

  if (clearBtn) {
    clearBtn.addEventListener("click", () => {
      out.value = "";
      saveState();
    });
  }
  if (copyBtn) {
    copyBtn.addEventListener("click", async () => {
      try {
        await navigator.clipboard.writeText(out.value || "");
        if (hint) hint.textContent = "Copied.";
        setTimeout(() => { if (hint) hint.textContent = ""; }, 1200);
      } catch (e) {
        if (hint) hint.textContent = "Copy failed.";
      }
    });
  }

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    const id = clientId;
    const command = (cmd.value || "").trim();
    if (!id || !command) return;

    if (hint) hint.textContent = "Running…";
    const res = await fetch(`/ui/api/clients/${encodeURIComponent(id)}/exec`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ cmd: command }),
    });
    const data = await res.json().catch(() => null);
    if (!data) {
      if (hint) hint.textContent = "Error.";
      return;
    }
    if (!data.ok) {
      out.value = "";
      if (hint) hint.textContent = data.error || (data.errors || []).join("\n") || "Failed.";
      saveState();
      return;
    }
    out.value = data.output || "";
    if (hint) hint.textContent = `OK (${(data.elapsed_ms || 0)}ms)`;
    saveState();
  });
})();

