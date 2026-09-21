"use strict";

// Called by Settings rendering. Files travel only between the browser and the local agent.
function setupBackupUI() {
  const box = document.createElement("section");
  box.className = "panel";
  box.innerHTML = `<div class="panel-head"><div><h2>Configuration backup</h2><p>설정 Export / Import · 로컬 파일로 백업하고 가져오세요.</p></div></div>
    <div class="panel-body"><p class="note">개인 키 원문과 연결 기록은 제외됩니다. 백업에는 내부 주소·사용자명·키 경로가 포함되므로 안전하게 보관하고 Git에 올리지 마세요.</p>
    <div class="actions"><button type="button" id="backup-export">↓ Export settings</button></div>
    <hr><label>SSHDesk backup file<input type="file" id="backup-file" accept=".json,application/json"></label>
    <label class="check-label"><input type="checkbox" id="backup-settings">이 PC의 SSH config / known_hosts 경로 설정도 가져오기</label>
    <p class="muted">동일한 ID와 내용은 건너뜁니다. 다른 내용의 ID 또는 Host 이름이 충돌하면 전체 가져오기를 취소하며 기존 항목을 덮어쓰지 않습니다.</p>
    <div class="actions"><button type="button" id="backup-preview" disabled>Import preview</button><button type="button" id="backup-apply" class="primary" disabled>Import configuration</button></div>
    <div id="backup-summary" aria-live="polite"></div></div>`;
  $("#content").append(box);
  const file = box.querySelector("#backup-file");
  const settings = box.querySelector("#backup-settings");
  const preview = box.querySelector("#backup-preview");
  const apply = box.querySelector("#backup-apply");
  const summary = box.querySelector("#backup-summary");
  let pending = null;
  function reset() { pending = null; apply.disabled = true; preview.disabled = !file.files.length; summary.replaceChildren(); }
  file.onchange = reset;
  settings.onchange = reset;
  box.querySelector("#backup-export").onclick = async (e) => {
    const button = e.currentTarget;
    button.disabled = true;
    try {
      const backup = await api("backup/export", "POST");
      const url = URL.createObjectURL(new Blob([JSON.stringify(backup, null, 2)], {type: "application/json"}));
      const link = document.createElement("a");
      link.href = url;
      link.download = `sshdesk-${new Date().toISOString().replace(/[:.]/g, "-")}.sshdesk.json`;
      document.body.append(link); link.click(); link.remove();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
      toast("설정 백업을 다운로드했습니다. 개인 키 파일은 포함되지 않습니다.");
    } catch (error) { toast(error.message, true); }
    finally { button.disabled = false; }
  };
  preview.onclick = async () => {
    preview.disabled = true; apply.disabled = true; pending = null;
    file.disabled = true; settings.disabled = true;
    try {
      const selected = file.files[0];
      if (!selected || selected.size > 1024 * 1024) throw new Error("1 MiB 이하의 SSHDesk JSON 백업을 선택하세요.");
      const request = {backup: JSON.parse(await selected.text()), import_settings: settings.checked};
      const result = await api("backup/preview", "POST", request);
      summary.innerHTML = `<h3>추가 예정</h3><p>${Object.entries(result.added).map(([kind, count]) => `${esc(kind)}: ${count}`).join(" · ")}</p>
        <p>Hosts: ${(request.backup.hosts || []).map(h => esc(h.name) + " " + env(h.environment)).join(", ") || "없음"}</p>
        ${result.warnings.map(w => `<p class="note">${esc(w)}</p>`).join("")}`;
      pending = request; apply.disabled = false;
    } catch (error) { summary.textContent = error.message; toast(error.message, true); }
    finally { preview.disabled = false; file.disabled = false; settings.disabled = false; }
  };
  apply.onclick = async () => {
    if (!pending) return;
    apply.disabled = true; preview.disabled = true; file.disabled = true; settings.disabled = true;
    try {
      await api("backup/apply", "POST", pending);
      await refresh();
      toast("설정을 가져왔습니다. 연결 전 키 경로와 서버 지문을 확인하세요.");
    } catch (error) { toast(error.message, true); apply.disabled = false; }
    finally { preview.disabled = false; file.disabled = false; settings.disabled = false; }
  };
}
