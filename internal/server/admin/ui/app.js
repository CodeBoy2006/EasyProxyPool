(() => {
  const state = {
    authMode: "token", // token | basic
    token: "",
    user: "",
    pass: "",
    logsPaused: false,
    logsLevel: "info",
    lastLogId: 0,
    es: null,
    nodes: [],
  };

  const el = (id) => document.getElementById(id);
  const authCard = el("auth");
  const authModeSel = el("auth-mode");
  const tokenWrap = el("token-wrap");
  const basicUserWrap = el("basic-user-wrap");
  const basicPassWrap = el("basic-pass-wrap");
  const authToken = el("auth-token");
  const authUser = el("auth-user");
  const authPass = el("auth-pass");
  const authErr = el("auth-error");

  const updaterSummary = el("updater-summary");
  const poolSummary = el("pool-summary");
  const nodesSummary = el("nodes-summary");
  const nodesFilter = el("nodes-filter");
  const nodesTbody = el("nodes-tbody");
  const btnRefresh = el("btn-refresh");

  const logsPre = el("logs");
  const logsLevelSel = el("logs-level");
  const btnLogsToggle = el("btn-logs-toggle");
  const btnLogsClear = el("btn-logs-clear");
  const logsHint = el("logs-hint");
  const btnLogin = el("btn-login");
  const btnLogout = el("btn-logout");

  function loadAuth() {
    const raw = localStorage.getItem("epp_admin_auth");
    if (!raw) return;
    try {
      const v = JSON.parse(raw);
      state.authMode = v.mode || "token";
      state.token = v.token || "";
      state.user = v.user || "";
      state.pass = v.pass || "";
    } catch {}
  }

  function saveAuth() {
    localStorage.setItem(
      "epp_admin_auth",
      JSON.stringify({ mode: state.authMode, token: state.token, user: state.user, pass: state.pass })
    );
  }

  function clearAuth() {
    localStorage.removeItem("epp_admin_auth");
    state.token = "";
    state.user = "";
    state.pass = "";
  }

  function setAuthUI() {
    authModeSel.value = state.authMode;
    authToken.value = state.token;
    authUser.value = state.user;
    authPass.value = state.pass;
    if (state.authMode === "basic") {
      tokenWrap.classList.add("hidden");
      basicUserWrap.classList.remove("hidden");
      basicPassWrap.classList.remove("hidden");
    } else {
      tokenWrap.classList.remove("hidden");
      basicUserWrap.classList.add("hidden");
      basicPassWrap.classList.add("hidden");
    }
  }

  function showAuth(show, message) {
    authCard.classList.toggle("hidden", !show);
    if (message) {
      authErr.textContent = message;
      authErr.classList.remove("hidden");
    } else {
      authErr.classList.add("hidden");
    }
  }

  function authHeader() {
    if (state.authMode === "basic") {
      const raw = `${state.user}:${state.pass}`;
      return "Basic " + btoa(unescape(encodeURIComponent(raw)));
    }
    if (state.token) return "Bearer " + state.token;
    return "";
  }

  async function apiGet(path) {
    const headers = {};
    const ah = authHeader();
    if (ah) headers["Authorization"] = ah;
    const res = await fetch(path, { headers, cache: "no-store" });
    if (res.status === 401) throw new Error("unauthorized");
    if (!res.ok) throw new Error(`http_${res.status}`);
    return res.json();
  }

  function fmtTime(s) {
    if (!s) return "-";
    return s;
  }

  function renderNodes() {
    const q = (nodesFilter.value || "").trim().toLowerCase();
    const filtered = state.nodes.filter((n) => (q ? n.id.toLowerCase().includes(q) : true));
    nodesTbody.innerHTML = filtered
      .map(
        (n) => `<tr>
          <td class="mono">${escapeHtml(n.id)}</td>
          <td>${n.alive ? "yes" : "no"}</td>
          <td class="mono">${n.delay_ms}</td>
          <td class="mono">${fmtTime(n.last_seen_utc)}</td>
          <td class="mono">${fmtTime(n.last_try_utc)}</td>
        </tr>`
      )
      .join("");
  }

  function escapeHtml(s) {
    return String(s)
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;")
      .replaceAll("'", "&#39;");
  }

  async function refreshStatus() {
    const st = await apiGet("/api/status");
    const up = st.updater || {};
    updaterSummary.textContent =
      `last_start=${up.LastUpdateStart || ""} last_end=${up.LastUpdateEnd || ""} ` +
      `err=${up.LastUpdateErr || "none"} fetched=${up.LastFetched ?? ""}`;
    const p = st.pool || {};
    poolSummary.textContent = `pool(total=${p.Total ?? ""} disabled=${p.Disabled ?? ""})`;
  }

  async function refreshNodes() {
    const ns = await apiGet("/api/nodes");
    nodesSummary.textContent =
      `alive=${ns.nodes_alive ?? 0}/${ns.nodes_total ?? 0} updated_at=${ns.updated_at_utc || "-"}`;
    state.nodes = ns.nodes || [];
    renderNodes();
  }

  function appendLogLine(line) {
    if (state.logsPaused) return;
    const maxLines = 500;
    const lines = logsPre.textContent.split("\n");
    lines.push(line);
    while (lines.length > maxLines) lines.shift();
    logsPre.textContent = lines.join("\n");
    logsPre.scrollTop = logsPre.scrollHeight;
  }

  function stopLogs() {
    if (state.es) {
      state.es.close();
      state.es = null;
    }
  }

  function startLogs() {
    stopLogs();
    if (state.authMode !== "token" || !state.token) {
      logsHint.textContent = "Live logs require shared_token mode (EventSource cannot set headers).";
      return;
    }
    logsHint.textContent = "";
    const url =
      `/api/events/logs?token=${encodeURIComponent(state.token)}` +
      `&since=${state.lastLogId}` +
      `&level=${encodeURIComponent(state.logsLevel)}`;
    const es = new EventSource(url);
    state.es = es;
    es.addEventListener("log", (ev) => {
      try {
        const obj = JSON.parse(ev.data);
        state.lastLogId = obj.id || state.lastLogId;
        const attrs = obj.attrs ? " " + JSON.stringify(obj.attrs) : "";
        appendLogLine(`[${obj.time_utc}] ${obj.level} ${obj.msg}${attrs}`);
      } catch {
        appendLogLine(ev.data);
      }
    });
    es.onerror = () => {
      // Keep it simple: browser will retry. We just show a hint.
      logsHint.textContent = "SSE disconnected (will retry).";
    };
  }

  async function connect() {
    try {
      await refreshStatus();
      await refreshNodes();
      showAuth(false);
      startLogs();
    } catch (e) {
      showAuth(true, e && e.message === "unauthorized" ? "Unauthorized. Check credentials." : "");
      stopLogs();
    }
  }

  // Events
  authModeSel.addEventListener("change", () => {
    state.authMode = authModeSel.value;
    setAuthUI();
  });
  btnLogin.addEventListener("click", async () => {
    state.authMode = authModeSel.value;
    state.token = authToken.value.trim();
    state.user = authUser.value.trim();
    state.pass = authPass.value;
    saveAuth();
    setAuthUI();
    await connect();
  });
  btnLogout.addEventListener("click", () => {
    clearAuth();
    stopLogs();
    showAuth(true);
    setAuthUI();
  });
  btnRefresh.addEventListener("click", async () => {
    await connect();
  });
  nodesFilter.addEventListener("input", renderNodes);
  logsLevelSel.addEventListener("change", () => {
    state.logsLevel = logsLevelSel.value;
    startLogs();
  });
  btnLogsToggle.addEventListener("click", () => {
    state.logsPaused = !state.logsPaused;
    btnLogsToggle.textContent = state.logsPaused ? "Resume" : "Pause";
  });
  btnLogsClear.addEventListener("click", () => {
    logsPre.textContent = "";
    state.lastLogId = 0;
    startLogs();
  });

  // Init
  loadAuth();
  setAuthUI();
  logsLevelSel.value = state.logsLevel;
  showAuth(true);

  setInterval(() => connect(), 4000);
  setInterval(() => refreshNodes().catch(() => {}), 6000);
  connect();
})();
