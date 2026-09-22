"use strict";

let settingsFileDirty = false;
window.addEventListener("beforeunload", event => {
  if (settingsFileDirty) { event.preventDefault(); event.returnValue = ""; }
});

function setupSettingsEditor() {
  const root = $(".settings-editor");
  const editor = $("#settings-editor-text");
  const save = $("#settings-editor-save");
  const reload = $("#settings-editor-reload");
  const status = $("#settings-editor-status");
  const links = [...document.querySelectorAll("[data-settings-open]")];
  let opened = null, original = "", busy = false;
  settingsFileDirty = false;
  const update = () => {
    save.disabled = busy || !opened || !settingsFileDirty;
    reload.disabled = busy || !opened;
    editor.disabled = busy || !opened;
    links.forEach(link => { link.disabled = busy; });
  };
  const open = async kind => {
    if (settingsFileDirty && !confirm("저장하지 않은 수정 내용을 버리고 파일을 다시 열까요?")) return;
    const form = $("#settings-form");
    if ($(`#settings-${kind}-file`).files.length || form.elements[kind].value !== state.settings[kind]) {
      toast("변경한 파일이나 경로를 먼저 Save settings로 저장하세요", true); return;
    }
    busy = true; update(); status.textContent = "읽는 중…";
    try {
      const result = await api("settings/editor/read", "POST", {kind});
      if (!root.isConnected) return;
      opened = {...result, kind}; original = result.content;
      editor.value = original; settingsFileDirty = false;
      $("#settings-editor-path").textContent = result.path;
      status.textContent = result.revision === "missing" ? "새 파일 · 저장하면 생성됩니다" : "파일을 열었습니다";
    } catch (err) { if (root.isConnected) status.textContent = err.message; }
    finally { busy = false; if (root.isConnected) update(); }
  };
  links.forEach(link => { link.onclick = () => open(link.dataset.settingsOpen); });
  editor.oninput = () => {
    settingsFileDirty = editor.value !== original;
    status.textContent = settingsFileDirty ? "저장하지 않은 변경 사항" : "";
    update();
  };
  reload.onclick = () => open(opened.kind);
  save.onclick = async () => {
    if (!opened || busy) return;
    const content = editor.value;
    busy = true; update(); status.textContent = "저장 중…";
    try {
      const result = await api("settings/editor/save", "POST", {kind:opened.kind, path:opened.path, revision:opened.revision, content});
      if (!root.isConnected) return;
      opened.revision = result.revision; original = content; settingsFileDirty = false;
      status.textContent = "저장했습니다"; toast("파일을 저장했습니다");
    } catch (err) { if (root.isConnected) status.textContent = err.message; }
    finally { busy = false; if (root.isConnected) update(); }
  };
}
