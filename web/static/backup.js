"use strict";

// Called by Settings rendering. Files travel only between the browser and the local agent.
function setupBackupUI() {
  const box = document.createElement("section");
  box.className = "panel";
  box.innerHTML = `<div class="panel-head"><div><h2>Configuration backup</h2><p>설정 Export / Import · 로컬 파일로 백업하고 가져오세요.</p></div></div>
    <div class="panel-body">
    <div class="backup-file-row"><label for="backup-name">설정파일</label>
      <div class="backup-controls"><input id="backup-name" type="text" readonly placeholder="설정파일을 선택하세요 (.json)">
      <input type="file" id="backup-file" accept=".json,application/json" hidden>
      <button type="button" id="backup-select">설정파일 선택</button>
      <button type="button" id="backup-export">내보내기</button></div>
      <div class="backup-import-actions">
      <button type="button" id="backup-preview" disabled>Preview</button>
      <button type="button" id="backup-close" hidden>닫기</button>
      <button type="button" id="backup-apply" class="primary" disabled>Import configuration</button></div></div>
    <div id="backup-summary" aria-live="polite"></div></div>`;
  $("#content").append(box);
  const file = box.querySelector("#backup-file");
  const select = box.querySelector("#backup-select");
  const name = box.querySelector("#backup-name");
  const preview = box.querySelector("#backup-preview");
  const apply = box.querySelector("#backup-apply");
  const summary = box.querySelector("#backup-summary");
  let pending = null;
  function reset() { pending = null; apply.disabled = true; preview.disabled = !file.files.length; name.value = file.files[0]?.name || ""; summary.replaceChildren(); box.querySelector("#backup-close").hidden = true; }
  box.querySelector("#backup-close").onclick = () => { reset(); preview.focus(); };
  select.onclick = () => file.click();
  file.onchange = reset;
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
    file.disabled = true; select.disabled = true;
    try {
      const selected = file.files[0];
      if (!selected || selected.size > 1024 * 1024) throw new Error("1 MiB 이하의 SSHDesk JSON 백업을 선택하세요.");
      const request = {backup: JSON.parse(await selected.text()), import_settings: false};
      const result = await api("backup/preview", "POST", request);
      summary.innerHTML = `<h3>추가 예정</h3><p>${Object.entries(result.added).map(([kind, count]) => `${esc(kind)}: ${count}`).join(" · ")}</p>
        <p>Hosts: ${(request.backup.hosts || []).map(h => esc(h.name) + " " + env(h.environment)).join(", ") || "없음"}</p>
        ${result.warnings.map(w => `<p class="note">${esc(w)}</p>`).join("")}`;
      pending = request; apply.disabled = false; box.querySelector("#backup-close").hidden = false;
    } catch (error) { summary.textContent = error.message; toast(error.message, true); }
    finally { preview.disabled = !file.files.length; file.disabled = false; select.disabled = false; }
  };
  apply.onclick = async () => {
    if (!pending) return;
    apply.disabled = true; preview.disabled = true; file.disabled = true; select.disabled = true;
    try {
      await api("backup/apply", "POST", pending);
      await refresh();
      toast("설정을 가져왔습니다. 연결 전 키 경로와 서버 지문을 확인하세요.");
    } catch (error) { toast(error.message, true); apply.disabled = false; }
    finally { preview.disabled = !file.files.length; file.disabled = false; select.disabled = false; }
  };
}
