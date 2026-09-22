"use strict";
const $ = (s) => document.querySelector(s);
const esc = (s) =>
  String(s ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
const token = $("meta[name=csrf-token]").content;
let state = {
    hosts: [],
    keys: [],
    tunnels: [],
    services: [],
    history: [],
    settings: {},
  },
  tunnelStatus = {},
  startupImport = null,
  teamSync = [],
  current = "dashboard",
  filter = "",
  envFilter = "",
  editing = null,
  busy = false;
let term = null,
  fit = null,
  socket = null,
  terminalHost = null,
  terminalAt = null,
  terminalPhase = "Disconnected",
  terminalGeneration = 0;
const pages = {
  dashboard: ["Dashboard", "호스트부터 내부 서비스까지, 한 곳에서 연결하세요."],
  hosts: ["Hosts", "Bastion과 내부 서버를 등록하고, 경유 연결을 관리하세요."],
  tunnels: ["Tunnels", "복잡한 포트 포워딩을 클릭 한 번으로 시작하세요."],
  terminal: ["Terminal", "브라우저에서 바로 연결하는 SSH 터미널."],
  keys: ["Keys", "키는 내 컴퓨터에. SSHDesk에는 파일 경로만 저장됩니다."],
  settings: ["Settings", "로컬 워크스페이스와 SSH 설정을 관리하세요."],
  sharing: ["팀 공유", "같은 로컬망의 팀원이 실행파일과 설정을 다운로드합니다."],
};
const env = (v) => `<span class="env env-${esc(v)}">${esc(v)}</span>`;
const status = (v) =>
  `<span class="status ${esc(v)}">${esc({ running: "Running", connected: "Connected", stopped: "Stopped", error: "Error", connecting: "Connecting", reconnecting: "Reconnecting", unknown: "Not checked" }[v] || v)}</span>`;
const hostName = (id) => state.hosts.find((h) => h.id === id)?.name || "—";
const tunnelName = (id) => state.tunnels.find((t) => t.id === id)?.name || "—";
const hostStatus = (id) =>
  state.history.find((h) => h.host_id === id)?.status || "unknown";
const ts = (id) =>
  tunnelStatus[id] || { state: "stopped", last_error: "", started_at: "" };
const button = (label, action, id = "", kind = "", cls = "") =>
  `<button type="button" class="${cls}" data-action="${action}" data-id="${esc(id)}" data-kind="${kind}">${label}</button>`;
const empty = (icon, title, text, action = "") =>
  `<div class="empty"><div class="empty-icon">${icon}</div><strong>${title}</strong><p>${text}</p>${action}</div>`;
const panel = (title, sub, body, action = "") =>
  `<section class="panel"><div class="panel-head"><div><h2>${title}</h2>${sub ? `<p>${sub}</p>` : ""}</div>${action}</div>${body}</section>`;
async function api(path, method = "GET", data) {
  const options = { method, headers: { "X-CSRF-Token": token } };
  if (method === "POST") {
    options.headers["Content-Type"] = "application/json";
    options.body = JSON.stringify(data ?? {});
  }
  const res = await fetch("/api/" + path, options);
  let value;
  const text = await res.text();
  try {
    value = JSON.parse(text);
  } catch {
    value = { error: text };
  }
  if (!res.ok) throw new Error(value.error || "요청에 실패했습니다");
  return value;
}
function toast(message, error = false) {
  const t = $("#toast");
  t.textContent = message;
  t.classList.toggle("error", error);
  t.hidden = false;
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => (t.hidden = true), error ? 9000 : 4000);
}
async function refresh(render = true) {
  const data = await api("state");
  state = data.data;
  tunnelStatus = data.tunnel_status;
  startupImport = data.startup_import;
  teamSync = data.team_sync || [];
  if (current === "sharing") {
    renderTeamSync();
    const info = await api("sharing");
    const peers = $("#share-peer-list");
    if (peers) peers.innerHTML = '<p class="muted">최근 5분간 원본 설정을 확인한 IP입니다. 같은 IP의 여러 앱은 하나로 표시됩니다.</p>' + ((info.peers || []).map(p => `<p>${esc(p.ip)} · ${esc(new Date(p.last_seen).toLocaleTimeString())}</p>`).join("") || "<p>아직 동기화 요청이 없습니다.</p>");
  }
  $("#host-count").textContent = state.hosts.length;
  $("#tunnel-count").textContent = Object.values(tunnelStatus).filter(
    (t) => t.state === "running",
  ).length;
  if (render) renderPage();
}
function navigate() {
  const hash = location.hash.slice(1).split("/");
  current = hash[0] === "services" ? "tunnels" : (pages[hash[0]] ? hash[0] : "dashboard");
  filter = "";
  envFilter = "";
  renderPage();
  if (current === "terminal" && hash[1] && terminalHost?.id !== hash[1]) {
    const h = state.hosts.find((h) => h.id === hash[1]);
    if (h) connectTerminal(h);
  }
}
function renderPage() {
  for (const a of document.querySelectorAll("nav a"))
    a.classList.toggle("active", a.dataset.page === current);
  $("#breadcrumb").textContent = pages[current][0];
  $("#page-title").textContent = pages[current][0];
  $("#page-subtitle").textContent = pages[current][1];
  $("#terminal-panel").hidden = current !== "terminal" || !terminalHost;
  $("#content").hidden = current === "terminal" && !!terminalHost;
  const actions = $("#page-actions");
  actions.innerHTML = "";
  if (["hosts", "tunnels", "keys"].includes(current)) {
    if (current === "hosts")
      actions.innerHTML = button("↓ Import SSH config", "import");
    actions.innerHTML += button(
      "+ Add " +
        { hosts: "host", tunnels: "tunnel", services: "service", keys: "key" }[
          current
        ],
      "add",
      "",
      current,
      "primary",
    );
  } else if (current === "dashboard") {
    actions.innerHTML =
      button("↻ Refresh", "refresh") +
      button("+ Add host", "add", "", "hosts", "primary");
  }
  if (current === "dashboard") dashboard();
  else if (current === "settings") settings();
  else if (current === "sharing") sharing();
  else if (current === "terminal") {
    if (!terminalHost)
      $("#content").innerHTML = panel(
        "Choose a host",
        "연결할 서버를 선택하세요.",
        hostTable(state.hosts, true),
      );
    else setTimeout(() => fit?.fit(), 0);
  } else collection();
}
function dashboard() {
  const reachable = state.hosts.filter(
    (h) => hostStatus(h.id) === "connected",
  ).length;
  const running = Object.values(tunnelStatus).filter(
    (t) => t.state === "running",
  ).length;
  const errors = state.history.filter((h) => h.status === "error").slice(0, 4);
  const metrics = [
    ["Registered hosts", state.hosts.length, "등록된 SSH 호스트", "▤"],
    ["Reachable hosts", reachable, "최근 SSH 연결 검사 기준", "◉"],
    ["Active tunnels", running, "로컬 포워딩 실행 중", "⇄"],
    ["Registered tunnels", state.tunnels.length, "등록된 포트 연결", "⇄"],
  ];
  $("#content").innerHTML =
    `<div class="cards">${metrics.map((m, i) => `<div class="metric ${i === 2 ? "active" : ""}"><div class="metric-label">${m[0]}</div><span class="metric-icon">${m[3]}</span><div class="metric-number">${m[1].toString().padStart(2, "0")}</div><small>${m[2]}</small></div>`).join("")}</div>` +
    (!state.hosts.length
      ? `<div class="onboarding"><div class="empty-icon">›_</div><div><h2>Your workspace starts here</h2><p>SSH 키를 등록한 뒤 첫 호스트를 추가하세요. 기존 SSH config도 가져올 수 있습니다.</p></div>${button("Register SSH key →", "add", "", "keys", "primary")}</div>`
      : "") +
    `<div class="dashboard-grid"><div>${panel('Hosts <span class="count-pill">' + state.hosts.length + "</span>", "가장 최근의 SSH 연결 상태", hostTable(state.hosts.slice(0, 5), true), '<a href="#hosts">View all →</a>')}${panel("Tunnel overview", "내부 네트워크로 연결되는 로컬 포트", tunnelTable(state.tunnels.slice(0, 4), true), '<a href="#tunnels">Manage →</a>')}</div><div>${panel(
      "Recent connections",
      "최근 연결 및 확인 내역",
      state.history.length
        ? `<div class="panel-body">${state.history
            .slice(0, 5)
            .map(
              (h) =>
                `<div class="activity"><span class="activity-icon ${h.status === "error" ? "error" : ""}">${h.status === "error" ? "!" : "↗"}</span><div class="activity-body"><strong>${esc(h.host_name)}</strong><p>${h.status === "error" ? esc(h.error) : "SSH connection established"}</p></div><time>${esc(new Date(h.at).toLocaleTimeString("ko-KR", { hour: "2-digit", minute: "2-digit" }))}</time></div>`,
            )
            .join("")}</div>`
        : empty(
            "◷",
            "아직 연결 기록이 없습니다",
            "호스트를 확인하거나 터미널을 시작하면 여기에 표시됩니다.",
          ),
    )}${panel("Connection health", "워크스페이스 상태", `<div class="panel-body"><div class="health-row">Local agent ${status("running")}</div><div class="health-row">SSH errors <strong class="${errors.length ? "error" : ""}">${state.history.filter((h) => h.status === "error").length}</strong></div><div class="health-row">Bind address <span class="mono">127.0.0.1</span></div>${errors.map((h) => `<p class="error wrap">${esc(h.host_name)} · ${esc(h.error)}</p>`).join("")}<div class="env-legend">${["LOCAL", "DEV", "STG", "PROD"].map(env).join("")}</div></div>`)}</div></div>`;
}
function toolbar() {
  return `<div class="toolbar"><input id="search" aria-label="검색" placeholder="이름이나 주소로 검색…" value="${esc(filter)}">${current === "hosts" ? `<select id="env-filter" aria-label="환경 필터"><option value="">All environments</option>${["LOCAL", "DEV", "STG", "PROD"].map((e) => `<option ${envFilter === e ? "selected" : ""}>${e}</option>`).join("")}</select>` : ""}<span class="muted">${state[current].length} ${current}</span></div>`;
}
function filtered() {
  return state[current].filter(
    (v) =>
      (!envFilter || v.environment === envFilter) &&
      JSON.stringify(v).toLowerCase().includes(filter.toLowerCase()),
  );
}
function collection() {
  const items = filtered();
  let body;
  if (current === "hosts") body = hostTable(items);
  if (current === "tunnels") body = tunnelTable(items);
  if (current === "keys") body = keyTable(items);
  $("#content").innerHTML =
    toolbar() +
    `<section class="panel">${body}</section>`;
  $("#search").oninput = (e) => {
    const pos = e.target.selectionStart;
    filter = e.target.value;
    collection();
    $("#search").focus();
    $("#search").setSelectionRange(pos, pos);
  };
  if ($("#env-filter"))
    $("#env-filter").onchange = (e) => {
      envFilter = e.target.value;
      collection();
    };
}
function sharedStatus(kind, id) {
  return teamSync.map(s => s.items?.[kind + "/" + id]).find(Boolean) || "";
}
function sharedClass(kind, id) {
  return sharedStatus(kind, id) === "missing" ? "source-missing" : "";
}
function sharedBadge(kind, id) {
  const value = sharedStatus(kind, id);
  return value ? `<span class="sync-badge ${value === "missing" ? "missing" : ""}" title="${value === "missing" ? "원본의 공유 범위에서 제거됐지만 내 PC에는 보관된 항목입니다." : "원본의 추가·수정 사항이 자동 적용됩니다. 실행 중 연결은 다음 연결부터 적용됩니다."}">${value === "missing" ? "원본에서 제거됨 · 보관" : "Master 동기화"}</span>` : "";
}
function hostTable(items, compact = false) {
  if (!items.length)
    return empty(
      "▤",
      "등록된 호스트가 없습니다",
      "SSH Key를 먼저 등록하고 Host를 추가하세요.",
      button("+ Add host", "add", "", "hosts"),
    );
  return `<div class="table-wrap"><table><thead><tr><th>Name / Environment</th><th>Address</th>${compact ? "" : "<th>Jump host</th>"}<th>Status</th><th>Actions</th></tr></thead><tbody>${items.map((h) => `<tr class="${sharedClass("hosts", h.id)}"><td><span class="name-cell">${esc(h.name)}</span>${sharedBadge("hosts", h.id)}<span class="subline">${env(h.environment)} &nbsp; ${esc(h.type)}</span></td><td class="mono">${esc(h.address)}:${h.port}<span class="subline">${esc(h.username)}</span></td>${compact ? "" : `<td>${esc(hostName(h.jump_id))}</td>`}<td>${status(hostStatus(h.id))}</td><td><div class="actions">${button("›_ Terminal", "terminal", h.id, "", "small")}${compact ? "" : button("Check", "check", h.id, "hosts", "small ghost") + button("Edit", "edit", h.id, "hosts", "small ghost") + button("Delete", "delete", h.id, "hosts", "small ghost danger")}</div></td></tr>`).join("")}</tbody></table></div>`;
}
function tunnelTable(items, compact = false) {
  if (!items.length)
    return empty(
      "⇄",
      "활성화할 터널을 추가하세요",
      "API, DB, Redis에 필요한 포트를 안전하게 연결합니다.",
      button("+ Add tunnel", "add", "", "tunnels"),
    );
  return `<div class="table-wrap"><table><thead><tr><th>Name / Via</th><th>Local → Remote</th><th>Status</th><th>Actions</th></tr></thead><tbody>${items
    .map((t) => {
      const v = ts(t.id);
      const links = state.services.filter((s) => s.tunnel_id === t.id);
      const webButtons = links.map((s) => button("↗ " + esc(s.name) + (sharedStatus("services", s.id) === "missing" ? " · 원본에서 제거됨" : ""), "open", s.id, "services", "small " + sharedClass("services", s.id))).join("");
      return `<tr class="${sharedClass("tunnels", t.id)}"><td class="name-cell">${esc(t.name)}${sharedBadge("tunnels", t.id)}<span class="subline">via ${esc(hostName(t.host_id))}${t.auto_reconnect ? " · Auto reconnect" : ""}</span></td><td class="mono">127.0.0.1:${t.local_port}<span class="subline">→ ${esc(t.remote_host)}:${t.remote_port}</span></td><td>${status(v.state)}${v.config_changed ? '<span class="sync-badge missing">설정 변경됨 · Stop 후 Start</span>' : ""}${v.started_at ? `<span class="subline">${esc(new Date(v.started_at).toLocaleTimeString())} · ${v.connections || 0} streams</span>` : ""}${v.last_error ? `<span class="subline error wrap" title="${esc(v.last_error)}">${esc(v.last_error)}</span>` : ""}</td><td><div class="actions">${button(["running", "connecting", "reconnecting"].includes(v.state) ? "■ Stop" : "▷ Start", ["running", "connecting", "reconnecting"].includes(v.state) ? "stop" : "start", t.id, "tunnels", "small " + (v.state === "running" ? "" : "primary"))}${webButtons}${compact ? "" : button("Edit", "edit", t.id, "tunnels", "small ghost") + button("Delete", "delete", t.id, "tunnels", "small ghost danger")}</div></td></tr>`;
    })
    .join("")}</tbody></table></div>`;
}
function keyTable(items) {
  if (!items.length)
    return empty(
      "⚿",
      "등록된 SSH Key가 없습니다",
      "PEM, RSA, ED25519 개인 키 파일의 경로를 등록하세요.",
      button("+ Add key", "add", "", "keys"),
    );
  return `<div class="table-wrap"><table><thead><tr><th>Name</th><th>File path</th><th>Storage</th><th>Actions</th></tr></thead><tbody>${items.map((k) => `<tr class="${sharedClass("keys", k.id)}"><td class="name-cell">${esc(k.name)}${sharedBadge("keys", k.id)}</td><td class="mono">${esc(k.path)}</td><td><span class="status connected">Path only</span></td><td><div class="actions">${button("Validate", "check", k.id, "keys", "small")}${button("Edit", "edit", k.id, "keys", "small ghost")}${button("Delete", "delete", k.id, "keys", "small ghost danger")}</div></td></tr>`).join("")}</tbody></table></div>`;
}
function settings() {
  const v = state.settings;
  $("#content").innerHTML =
    `<div class="settings-grid">${panel("SSH settings", "원본 config와 known_hosts는 수정하지 않습니다.", `<form id="settings-form" class="panel-body"><label>Known hosts file<input name="known_hosts" required value="${esc(v.known_hosts)}"></label><label>SSH config file<input name="ssh_config" required value="${esc(v.ssh_config)}"></label><div class="actions"><button class="primary">Save settings</button>${button("↓ Import preview", "import")}</div></form>`)}${panel("Security defaults", "로컬에서 실행되는 개인용 연결 도구", `<div class="panel-body"><ul><li>HTTP 및 터널: 127.0.0.1 전용</li><li>SSH 서버 키: known_hosts 또는 검증된 SHA256 지문</li><li>키 원문 및 비밀번호 저장 안 함</li><li>CSRF / WebSocket Origin 검사</li><li>앱 종료 시 터미널과 터널 종료</li><li>시작 시 터널 자동 실행 안 함</li></ul><p class="note">PROD는 빨간색으로 표시됩니다. 가져온 Host의 기본 환경은 DEV이므로 실제 환경에 맞게 수정하세요.</p></div>`)}</div><div id="import-preview"></div>`;
  setupBackupUI();
  if (startupImport) {
    const r = startupImport;
    const report = document.createElement("section");
    report.className = "panel";
    report.innerHTML = `<div class="panel-head"><div><h2>Startup SSH import</h2><p>시작 시 SSH config 자동 등록 결과</p></div></div><div class="panel-body"><p class="mono wrap">${esc(r.source || "")}</p><p>${r.disabled ? "자동 등록 비활성화 (-no-auto-import)" : r.missing ? "SSH config 파일이 없습니다. 필요하면 수동으로 Host를 등록하세요." : `추가 ${r.imported} · 기존 유지 ${r.existing} · 건너뜀 ${(r.skipped || []).length}`}</p><p class="note">기존 등록 내용과 원본 파일은 보존합니다. 새 Host의 기본 환경은 DEV입니다. 연결 전 환경과 서버 키를 확인하세요.</p>${(r.warnings || []).map(w => `<p class="note">${esc(w)}</p>`).join("")}${(r.skipped || []).map(s => `<p class="wrap"><strong>${esc(s.name)}</strong> · ${esc(s.reason)}</p>`).join("")}</div>`;
    $("#content").prepend(report);
  }
  $("#settings-form").onsubmit = async (e) => {
    e.preventDefault();
    try {
      await api("settings", "POST", Object.fromEntries(new FormData(e.target)));
      await refresh(false);
      toast("설정을 저장했습니다");
    } catch (e) {
      toast(e.message, true);
    }
  };
}
async function sharing() {
  const content = $("#content");
  content.innerHTML = panel("팀 다운로드 페이지", "", '<div class="panel-body">공유 상태 확인 중…</div>');
  try {
    const info = await api("sharing");
    if (current !== "sharing") return;
    content.innerHTML = panel("팀 다운로드 페이지", info.enabled ? "공유 중" : "공유 꺼짐", `<div class="panel-body">
      <h3>팀 접속 주소</h3>${info.primary_url ? `<p><a href="${esc(info.primary_url)}" target="_blank" rel="noreferrer">${esc(info.primary_url)}</a></p>` : '<p>사용 가능한 로컬망 주소가 없습니다.</p>'}
      ${info.urls.length > 1 ? `<details><summary>다른 네트워크 주소</summary><p class="muted">가상 어댑터나 VPN 주소일 수 있습니다. 팀원과 연결된 네트워크 주소를 사용하세요.</p>${info.urls.filter(url => url !== info.primary_url).map(url => `<p>${esc(url)}</p>`).join("")}</details>` : ""}
      <h3>최근 동기화 PC</h3><div id="share-peer-list"><p class="muted">최근 5분간 원본 설정을 확인한 IP입니다. 같은 IP의 여러 앱은 하나로 표시됩니다.</p>${(info.peers || []).map(p => `<p>${esc(p.ip)} · ${esc(new Date(p.last_seen).toLocaleTimeString())}</p>`).join("") || "<p>아직 동기화 요청이 없습니다.</p>"}</div>
      <h3>포함할 SSH 키</h3>${state.keys.map(k => `<label class="check-label"><input type="checkbox" name="share-key" value="${esc(k.id)}" ${info.selected_keys.includes(k.id) ? "checked" : ""}>${esc(k.name)}</label>`).join("") || '<p>Keys에서 먼저 키를 등록하세요.</p>'}
      <div id="share-host-preview"></div>
      <p class="note">키 포함 배포본에는 선택한 개인 키 원문이 들어갑니다. 공유를 켜면 같은 로컬 서브넷에서 다운로드할 수 있습니다. HTTP 공유이므로 신뢰하는 로컬망에서만 사용하고 공개 저장소에는 올리지 마세요.</p>
      <div class="actions"><button type="button" id="share-prepare" ${state.keys.length ? "" : "disabled"}>선택한 키로 배포본 만들기</button><button type="button" id="share-toggle" class="primary">${info.enabled ? "공유 중지" : "공유 시작"}</button></div>
      <p>${info.prepared ? "키 포함 배포본 준비됨 · 받는 PC에서 처음 실행하면 자동 등록됩니다." : "현재 기본 실행파일과 전체 JSON 설정만 공유합니다. 개인 키는 포함되지 않습니다."}</p>
      <p class="muted">새 배포본으로 등록한 팀원에게 선택한 키 범위의 호스트·터널 추가와 수정이 30초마다 자동 반영됩니다. 원본에서 제거된 항목은 팀원 PC에 보관 표시로 남습니다. 개인 키 변경·추가는 새 배포본을 받아야 합니다. 공유 중지나 앱 종료 시 키 포함 배포본은 폐기되지만 구독 정보는 유지됩니다. 앱을 다시 켜고 공유를 시작하면 기존 구독이 재개됩니다. 관리 화면과 터미널은 공유되지 않습니다. Windows 방화벽은 TCP 9877을 로컬 서브넷에 허용해야 합니다.</p></div>`);
    content.insertAdjacentHTML("beforeend", '<div id="team-sync-panel"></div>');
    renderTeamSync();
    const selected = () => [...content.querySelectorAll('[name="share-key"]:checked')].map(input => input.value);
    const preview = () => {
      const ids = selected();
      const hosts = state.hosts.filter(h => ids.includes(h.key_id));
      $("#share-host-preview").innerHTML = `<p>포함할 호스트 ${hosts.length}개: ${hosts.map(h => esc(h.name)).join(", ") || "없음"}</p>`;
      $("#share-prepare").disabled = !ids.length;
    };
    content.querySelectorAll('[name="share-key"]').forEach(input => input.onchange = preview);
    preview();
    $("#share-prepare").onclick = async event => {
      const button = event.currentTarget; button.disabled = true;
      try { await api("sharing/prepare", "POST", {key_ids:selected()}); await sharing(); toast("키와 호스트를 포함한 배포본을 준비했습니다"); }
      catch (err) { toast(err.message,true); button.disabled=false; }
    };
    $("#share-toggle").onclick = async event => {
      const button = event.currentTarget; button.disabled = true;
      try { await api("sharing/" + (info.enabled ? "stop" : "start"), "POST"); await sharing(); }
      catch (err) { toast(err.message, true); button.disabled = false; }
    };
  } catch (err) { if (current === "sharing") content.textContent = err.message; }
}
function renderTeamSync() {
  const root = $("#team-sync-panel");
  if (!root || root.contains(document.activeElement)) return;
  root.innerHTML = panel("내 PC의 원본 구독", "새 팀 배포본을 실행하면 자동 등록됩니다.", `<div class="panel-body"><p>원본이 공유 중이고 이 앱이 실행 중일 때 30초마다 갱신합니다. 개인이 만든 항목·키 파일 경로는 유지하고, 공유 항목은 원본 기준으로 수정됩니다. 실행 중 터널·터미널은 끊지 않으며 다음 연결부터 적용됩니다.</p>${teamSync.length ? teamSync.map(s => `<div class="sync-source"><strong>${esc(s.url)}</strong><p>${s.paused ? "자동 갱신 일시 중지" : s.error ? esc(s.error) : s.last_sync ? "최근 갱신: " + esc(new Date(s.last_sync).toLocaleString()) : "첫 동기화 대기 중"}</p><p class="muted">원본에서 제거됨: ${Object.values(s.items || {}).filter(v => v === "missing").length}개 · 삭제하지 않고 보관합니다.</p><div class="actions"><button data-sync="${s.paused ? "resume" : "pause"}" data-source="${esc(s.id)}">${s.paused ? "자동 갱신 재개" : "자동 갱신 일시 중지"}</button><button data-sync="refresh" data-source="${esc(s.id)}">지금 확인</button></div><form class="sync-address" data-source="${esc(s.id)}"><label>원본 IP가 바뀐 경우<input name="url" aria-label="원본 주소" value="${esc(s.url)}" required></label><button>원본 주소 저장</button></form></div>`).join("") : '<p>구독 중인 원본이 없습니다. 기존 배포본 사용자는 새 버전의 팀 배포본을 한 번 받아 실행하세요. 일반 JSON 가져오기와 macOS SSH 텍스트 묶음은 자동 구독하지 않습니다.</p>'}</div>`);
  root.querySelectorAll("[data-sync]").forEach(button => button.onclick = async () => {
    button.disabled = true;
    try { await api("team-sync/" + button.dataset.sync, "POST", {id:button.dataset.source}); await refresh(false); button.blur(); renderTeamSync(); }
    catch (err) { toast(err.message, true); }
    finally { button.disabled = false; }
  });
  root.querySelectorAll(".sync-address").forEach(form => form.onsubmit = async e => {
    e.preventDefault();
    try { await api("team-sync/address", "POST", {id:form.dataset.source,url:form.elements.url.value}); toast("원본 주소를 저장했습니다. 원본 서명은 계속 검증합니다."); }
    catch (err) { toast(err.message, true); }
  });
}
const schemas = {
  hosts: [
    ["name", "Name", "text"],
    ["environment", "Environment", "env"],
    ["type", "Host type", "type"],
    ["address", "Hostname / IP", "text"],
    ["port", "SSH port", "number", 22],
    ["username", "Username", "text"],
    ["key_id", "SSH key", "keys"],
    ["jump_id", "Jump host", "hosts"],
    ["fingerprint", "Verified server fingerprint (optional)", "text"],
    ["description", "Description", "textarea"],
  ],
  tunnels: [
    ["name", "Name", "text"],
    ["host_id", "SSH host", "hosts-required"],
    ["local_host", "Local host", "text", "127.0.0.1"],
    ["local_port", "Local port", "number", 18080],
    ["remote_host", "Remote host", "text", "127.0.0.1"],
    ["remote_port", "Remote port", "number", 8080],
    ["auto_reconnect", "Auto reconnect", "checkbox", true],
    ["description", "Description", "textarea"],
  ],
  keys: [
    ["name", "Name", "text"],
    ["path", "Private key file path", "text"],
  ],
};
function editor(kind, id = "") {
  const item = state[kind].find((v) => v.id === id) || {};
  editing = { kind, id };
  $("#editor-title").textContent =
    (id ? "Edit " : "Add ") +
    { hosts: "host", tunnels: "tunnel", services: "service", keys: "key" }[
      kind
    ];
  $("#form-error").textContent = "";
  $("#editor-fields").innerHTML =
    schemas[kind]
      .map(([name, label, type, def]) => {
        const value = item[name] ?? def ?? "";
        let input;
        if (
          [
            "env",
            "type",
            "keys",
            "hosts",
            "hosts-required",
            "tunnels",
          ].includes(type)
        ) {
          let options;
          if (type === "env")
            options = ["LOCAL", "DEV", "STG", "PROD"].map((v) => [v, v]);
          else if (type === "type")
            options = ["Server", "Bastion", "Database", "Other"].map((v) => [
              v,
              v,
            ]);
          else
            options = state[type.startsWith("hosts") ? "hosts" : type]
              .filter((v) => (type === "hosts" ? v.id !== id : true))
              .map((v) => [v.id, v.name]);
          if (!["env", "type"].includes(type))
            options.unshift([
              "",
              type === "hosts" ? "Direct connection (none)" : "Select…",
            ]);
          input = `<select name="${name}" ${type === "hosts" ? "" : "required"}>${options.map(([v, t]) => `<option value="${esc(v)}" ${String(value || (type === "env" ? "DEV" : "")) === String(v) ? "selected" : ""}>${esc(t)}</option>`).join("")}</select>`;
        } else if (type === "textarea")
          input = `<textarea name="${name}" maxlength="2000">${esc(value)}</textarea>`;
        else if (type === "checkbox")
          input = `<input type="checkbox" name="${name}" ${value ? "checked" : ""}>`;
        else
          input = `<input name="${name}" type="${type}" value="${esc(value)}" ${name === "local_host" ? "readonly" : ""} ${type === "number" ? 'min="1" max="65535"' : ""} ${name === "fingerprint" ? 'placeholder="SHA256:…"' : "required"} maxlength="1024">`;
        if (kind === "keys" && name === "path")
          input = `<span class="file-path-picker">${input}<button type="button" id="pick-key-file">파일 선택…</button></span>`;
        return `<label class="${["description", "fingerprint", "path", "url"].includes(name) ? "full" : ""} ${type === "checkbox" ? "check-label" : ""}">${label}${input}</label>`;
      })
      .join("") +
    (kind === "hosts"
      ? '<p class="note full">알 수 없는 서버 키는 자동으로 신뢰하지 않습니다. known_hosts를 사용하거나 관리자에게 별도로 확인한 SHA256 지문을 입력하세요.</p>'
      : kind === "keys"
        ? '<p class="note full">로컬 파일 경로만 저장합니다. 암호화된 private key의 passphrase 입력은 향후 지원합니다.</p>'
        : "");
  if (kind === "tunnels") {
    const links = state.services.filter((s) => s.tunnel_id === id);
    const block = document.createElement("div");
    block.className = "full";
    block.innerHTML = `<h3>웹 바로가기 (선택)</h3><p class="muted">웹 서비스는 URL을 등록하고, DB 연결은 비워두세요. 기존 URL을 비우면 바로가기를 삭제합니다.</p>`;
    const addRow = (link = {}) => {
      const row = document.createElement("div");
      row.className = "tunnel-link-row";
      row.dataset.id = link.id || "";
      row.innerHTML = `<label>표시 이름<input data-link-name value="${esc(link.name || "")}" placeholder="예: Airflow" maxlength="160"></label><label>웹 URL<input data-link-url type="url" value="${esc(link.url || "")}" placeholder="http://127.0.0.1:18080" maxlength="1024"></label>`;
      block.append(row);
    };
    links.forEach(addRow);
    addRow();
    const add = document.createElement("button");
    add.type = "button"; add.textContent = "+ 웹 바로가기";
    add.onclick = () => addRow();
    block.append(add);
    $("#editor-fields").append(block);
  }
  if (kind === "keys") {
    const picker = $("#pick-key-file");
    const pathInput = $('#editor-fields input[name="path"]');
    picker.onclick = async () => {
      picker.disabled = true;
      picker.textContent = "선택 중…";
      $("#form-error").textContent = "";
      try {
        const result = await api("keys/pick-file", "POST");
        if (!result.cancelled && pathInput.isConnected && $("#editor").open)
          pathInput.value = result.path;
      } catch (err) {
        if (pathInput.isConnected) $("#form-error").textContent = err.message;
      } finally {
        picker.disabled = false;
        picker.textContent = "파일 선택…";
      }
    };
  }
  $("#editor").showModal();
}
$("#editor-close").onclick = $("#editor-cancel").onclick = () =>
  $("#editor").close();
$("#editor-form").onsubmit = async (e) => {
  e.preventDefault();
  const submit = e.target.querySelector("[type=submit]");
  submit.disabled = true;
  const data = Object.fromEntries(new FormData(e.target));
  data.id = editing.id;
  if (editing.kind === "tunnels") {
    data.web_links = [...document.querySelectorAll(".tunnel-link-row")].map((row) => ({
      id: row.dataset.id,
      name: row.querySelector("[data-link-name]").value.trim() || data.name,
      url: row.querySelector("[data-link-url]").value.trim(),
    })).filter((link) => link.url);
  }
  for (const [name, , type] of schemas[editing.kind]) {
    if (type === "number") data[name] = Number(data[name]);
    if (type === "checkbox") data[name] = e.target.elements[name].checked;
  }
  try {
    await api(editing.kind, "POST", data);
    $("#editor").close();
    await refresh();
    toast("저장했습니다");
  } catch (err) {
    $("#form-error").textContent = err.message;
  } finally {
    submit.disabled = false;
  }
};
async function importPreview() {
  if (current !== "settings") {
    location.hash = "#settings";
    current = "settings";
    renderPage();
  }
  const path = $("#settings-form").elements.ssh_config.value;
  const container = $("#import-preview");
  const preview = await api("import/preview", "POST", { path });
  if (!container.isConnected || current !== "settings") return;
  container.innerHTML = panel(
    "Import preview",
    "가져올 Host와 필요한 Jump Host를 선택하세요. 원본 파일은 변경하지 않습니다.",
    `<div class="panel-body">${preview.warnings.map((w) => `<p class="note">${esc(w)}</p>`).join("")}<div class="table-wrap"><table><thead><tr><th>Select / Alias</th><th>Address / User</th><th>Key / Jump</th><th>Notes</th></tr></thead><tbody>${preview.hosts.map((c) => `<tr><td><label class="check-label"><input type="checkbox" name="import-alias" value="${esc(c.alias)}" ${c.valid ? "" : "disabled"}>${esc(c.alias)}</label></td><td class="mono">${esc(c.address)}:${c.port}<span class="subline">${esc(c.username)}</span></td><td class="mono">${esc(c.identity_file)}<span class="subline">via ${esc(c.jumps.join(" → ") || "Direct")}</span></td><td class="wrap">${c.warnings.map(esc).join("<br>") || "Ready for import"}</td></tr>`).join("")}</tbody></table></div><p class="note">기존 별칭은 덮어쓰지 않습니다. 키 파일이 없거나 경유 Host가 누락되면 전체 가져오기를 취소합니다.</p>${button("Import selected hosts", "import-apply", "", "", "primary")}</div>`,
    button("닫기", "import-close", "", "", "ghost"),
  );
  container.dataset.path = path;
}
async function connectTerminal(h) {
  if (
    h.environment === "PROD" &&
    !confirm(`PROD 서버 ${h.name}에 연결합니다. 계속하시겠습니까?`)
  )
    return;
  disconnectTerminal();
  terminalHost = h;
  terminalAt = null;
  terminalPhase = "Connecting";
  $("#terminal-name").textContent = h.name;
  $("#terminal-env").innerHTML = env(h.environment);
  $("#terminal-address").textContent =
    `${h.username}@${h.address}:${h.port}${h.jump_id ? " via " + hostName(h.jump_id) : ""}`;
  $("#prod-warning").hidden = h.environment !== "PROD";
  $("#terminal-status").innerHTML = status("connecting");
  $("#terminal-time").textContent = "";
  renderPage();
  $("#terminal-screen").replaceChildren();
  term?.dispose();
  term = new Terminal({
    cursorBlink: true,
    fontSize: 14,
    fontFamily: 'Consolas, "Cascadia Code", monospace',
    theme: {
      background: "#080c12",
      foreground: "#d7e4ef",
      cursor: "#65dec0",
      red: "#ff7887",
      brightRed: "#ff9fab",
      brightBlack: "#8b9ab0",
    },
    scrollback: 5000,
    allowProposedApi: false,
  });
  fit = new FitAddon.FitAddon();
  term.loadAddon(fit);
  term.open($("#terminal-screen"));
  fit.fit();
  const generation = ++terminalGeneration;
  const ws = new WebSocket(
    `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/ws/terminal/${h.id}`,
  );
  socket = ws;
  ws.binaryType = "arraybuffer";
  term.writeln("\x1b[90mConnecting securely…\x1b[0m");
  ws.onopen = () =>
    ws.send(
      JSON.stringify({ type: "auth", token, cols: term.cols, rows: term.rows }),
    );
  ws.onmessage = (e) => {
    if (generation !== terminalGeneration) return;
    if (e.data instanceof ArrayBuffer) {
      term.write(new Uint8Array(e.data));
      return;
    }
    const v = JSON.parse(e.data);
    if (v.type === "status") {
      terminalAt = new Date(v.at);
      terminalPhase = "Connected";
      $("#terminal-status").innerHTML = status("connected");
      term.focus();
      refresh(false).catch(() => {});
    } else if (v.type === "error" || v.type === "exit") {
      terminalPhase = v.type === "error" ? "Error" : "Disconnected";
      term.writeln("\r\n\x1b[31m" + v.message + "\x1b[0m");
      $("#terminal-status").innerHTML = status(
        v.type === "error" ? "error" : "stopped",
      );
    }
  };
  ws.onerror = () => {
    if (generation === terminalGeneration)
      toast("터미널 연결 오류. Reconnect로 재시도하세요.", true);
  };
  ws.onclose = () => {
    if (generation !== terminalGeneration) return;
    terminalPhase = "Disconnected";
    $("#terminal-status").innerHTML = status("stopped");
    term.writeln(
      "\r\n\x1b[90mSession closed. Reconnect to start a new shell.\x1b[0m",
    );
    refresh(false).catch(() => {});
  };
  term.onData((data) => {
    if (ws.readyState === WebSocket.OPEN)
      ws.send(JSON.stringify({ type: "input", data }));
  });
  term.onResize(({ cols, rows }) => {
    if (ws.readyState === WebSocket.OPEN)
      ws.send(JSON.stringify({ type: "resize", cols, rows }));
  });
}
function disconnectTerminal() {
  if (socket) {
    socket.close();
    socket = null;
  }
  terminalGeneration++;
  terminalPhase = "Disconnected";
  $("#terminal-status").innerHTML = status("stopped");
}
$("#terminal-disconnect").onclick = () => {
  disconnectTerminal();
  term?.writeln("\r\nDisconnected.");
  toast("터미널 연결을 종료했습니다");
};
$("#terminal-reconnect").onclick = () =>
  terminalHost && connectTerminal(terminalHost);
new ResizeObserver(() => {
  if (current === "terminal" && term) fit?.fit();
}).observe($("#terminal-screen"));
setInterval(() => {
  if (terminalAt && terminalPhase === "Connected") {
    $("#terminal-time").textContent =
      Math.floor((Date.now() - terminalAt) / 1000) + "s connected";
  }
}, 1000);
document.addEventListener("click", async (e) => {
  const b = e.target.closest("button[data-action]");
  if (!b) return;
  const { action, id, kind } = b.dataset;
  if (action === "add" || action === "edit") {
    editor(kind, id);
    return;
  }
  if (action === "terminal") {
    const h = state.hosts.find((h) => h.id === id);
    if (h) {
      if (location.hash === `#terminal/${id}`) connectTerminal(h);
      else location.hash = `#terminal/${id}`;
    }
    return;
  }
  if (busy) return;
  if (action === "import-close") {
    const preview = $("#import-preview");
    if (preview) {
      preview.replaceChildren();
      delete preview.dataset.path;
    }
    $('#settings-form [data-action="import"]')?.focus();
    return;
  }
  busy = true;
  b.disabled = true;
  try {
    if (action === "delete") {
      if (
        !confirm(
          kind === "tunnels" ? "터널과 연결된 웹 바로가기를 함께 삭제하시겠습니까?" : "삭제하시겠습니까? 다른 항목에서 사용 중이면 삭제할 수 없습니다.",
        )
      )
        return;
      await api(kind + "/" + id, "DELETE");
      toast("삭제했습니다");
    } else if (action === "check") {
      await api(kind + "/" + id + "/check", "POST");
      toast(
        kind === "keys"
          ? "키 파일을 읽고 해석했습니다"
          : "SSH 인증과 서버 키 검증에 성공했습니다",
      );
    } else if (action === "start" || action === "stop") {
      await api("tunnels/" + id + "/" + action, "POST");
      toast(action === "start" ? "터널을 시작했습니다" : "터널을 중지했습니다");
    } else if (action === "open") {
      await api("services/" + id + "/open", "POST");
      toast("터널을 확인하고 기본 브라우저에서 열었습니다");
    } else if (action === "import") {
      await importPreview();
      return;
    } else if (action === "import-apply") {
      const path = $("#import-preview").dataset.path;
      const aliases = [
        ...document.querySelectorAll("[name=import-alias]:checked"),
      ].map((c) => c.value);
      const result = await api("import/apply", "POST", { path, aliases });
      toast(result.imported + "개 Host를 가져왔습니다");
      location.hash = "#hosts";
    }
    await refresh();
    if (action === "refresh") toast("대시보드를 새로고침했습니다.");
  } catch (err) {
    toast(err.message, true);
    await refresh(action !== "import" && action !== "import-apply").catch(
      () => {},
    );
  } finally {
    busy = false;
    b.disabled = false;
  }
});
window.addEventListener("hashchange", navigate);
window.addEventListener("beforeunload", () => disconnectTerminal());
setInterval(() => {
  if (!busy && !$("#editor").open) {
    const draw = ["dashboard", "hosts", "tunnels", "keys"].includes(current) && !filter && document.activeElement?.id !== "search" || (current === "terminal" && !terminalHost);
    refresh(draw).catch(() => {});
  }
}, 5000);
refresh(false)
  .then(navigate)
  .catch((e) => toast(e.message, true));
