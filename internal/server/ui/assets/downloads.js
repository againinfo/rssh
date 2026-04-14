(function () {
  document.querySelectorAll(".dl-del").forEach((btn) => {
    btn.addEventListener("click", async () => {
      const id = btn.dataset.id;
      if (!id) return;
      if (!confirm(`Delete download ${id}?`)) return;
      btn.disabled = true;
      const res = await fetch(`/ui/api/downloads/${encodeURIComponent(id)}/delete`, { method: "POST" });
      const data = await res.json().catch(() => null);
      if (!data || !data.ok) {
        alert((data && data.error) || "delete failed");
        btn.disabled = false;
        return;
      }
      location.reload();
    });
  });
})();

