(function () {
  const form = document.getElementById("build-form");
  const out = document.getElementById("build-result");
  const status = document.getElementById("build-status");

  function val(name) {
    const el = form.querySelector(`[name="${name}"]`);
    if (!el) return "";
    if (el.type === "checkbox") return el.checked;
    return el.value;
  }

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    if (status) status.textContent = "Building…";
    if (out) out.value = "";

    const body = {
      name: val("name"),
      comment: val("comment"),
      owners: val("owners"),
      goos: val("goos"),
      goarch: val("goarch"),
      goarm: val("goarm"),
      connect_back: val("connect_back"),
      transport: val("transport"),
      proxy: val("proxy"),
      fingerprint: val("fingerprint"),
      sni: val("sni"),
      log_level: val("log_level"),
      working_directory: val("working_directory"),
      ntlm_proxy_creds: val("ntlm_proxy_creds"),
      version_string: val("version_string"),
      use_host_header: val("use_host_header"),
      shared_object: val("shared_object"),
      garble: val("garble"),
      upx: val("upx"),
      lzma: val("lzma"),
      no_lib_c: val("no_lib_c"),
      raw_download: val("raw_download"),
      use_kerberos: val("use_kerberos"),
    };

    const res = await fetch("/ui/api/build", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(body),
    });
    const data = await res.json().catch(() => null);
    if (!data) {
      if (status) status.textContent = "Error: invalid response";
      return;
    }
    if (!data.ok) {
      if (status) status.textContent = "Failed";
      if (out) out.value = data.error || "unknown error";
      return;
    }
    if (status) status.textContent = "Done";
    if (out) out.value = data.result || "";
  });
})();

