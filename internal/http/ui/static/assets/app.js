const storageKey = "mssar.admin.session";
const pageSize = 10;

const viewConfigs = {
  apps: {
    kicker: "应用授权",
    title: "应用授权",
    endpoint: "/api/v1/admin/app",
    actions: ["create", "refresh"],
    filters: [
      { key: "app_id", label: "应用 ID", type: "number" },
      { key: "name", label: "应用名称", type: "text" }
    ]
  },
  logs: {
    kicker: "审计日志",
    title: "审计日志",
    endpoint: "/api/v1/admin/log",
    actions: ["refresh"],
    filters: [
      { key: "cate", label: "分类", type: "text", placeholder: "AUTH / APP / ES_DEBUG / REC_DEBUG" },
      { key: "type", label: "类型", type: "text" }
    ]
  },
  es: {
    kicker: "ES Debug",
    title: "ES Debug",
    actions: []
  },
  rec: {
    kicker: "推荐 Debug",
    title: "推荐 Debug",
    actions: []
  }
};

const state = {
  activeView: "apps",
  accessToken: "",
  adminID: 0,
  adminName: "",
  loginOpen: true,
  loginError: "",
  loading: false,
  feedback: "请先登录后查看数据。",
  feedbackKind: "",
  filters: {
    apps: {},
    logs: {}
  },
  page: {
    apps: 1,
    logs: 1
  },
  data: {
    apps: null,
    logs: null
  },
  appForm: null,
  appFormError: "",
  appSecretReveal: null,
  debug: {
    esMode: "index",
    esResult: "等待查询结果。",
    recResult: "等待查询结果。"
  }
};

const loginForm = document.getElementById("login-form");
const loginNameInput = document.getElementById("login-name");
const loginPasswordInput = document.getElementById("login-password");
const loginButton = document.getElementById("open-login-button");
const sessionPanel = document.getElementById("session-panel");
const sessionName = document.getElementById("session-name");
const logoutButton = document.getElementById("logout-button");
const panelActions = document.getElementById("panel-actions");
const viewKicker = document.getElementById("view-kicker");
const viewTitle = document.getElementById("view-title");
const panelFeedback = document.getElementById("panel-feedback");
const secretRevealPanel = document.getElementById("secret-reveal-panel");
const filterBar = document.getElementById("filter-bar");
const formPanel = document.getElementById("form-panel");
const panelContent = document.getElementById("panel-content");
const pagination = document.getElementById("pagination");
const menuItems = Array.from(document.querySelectorAll(".menu-item"));

function escapeHTML(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function decodeJWTPayload(token) {
  try {
    const parts = String(token || "").split(".");
    if (parts.length < 2) {
      return {};
    }
    const payload = parts[1]
      .replaceAll("-", "+")
      .replaceAll("_", "/")
      .padEnd(Math.ceil(parts[1].length / 4) * 4, "=");
    return JSON.parse(window.atob(payload));
  } catch (_error) {
    return {};
  }
}

function hasSession() {
  return Boolean(state.accessToken);
}

function persistSession() {
  if (!state.accessToken) {
    window.sessionStorage.removeItem(storageKey);
    return;
  }
  window.sessionStorage.setItem(
    storageKey,
    JSON.stringify({
      accessToken: state.accessToken,
      adminID: state.adminID,
      adminName: state.adminName
    })
  );
}

function restoreSession() {
  const raw = window.sessionStorage.getItem(storageKey);
  if (!raw) {
    return;
  }
  try {
    const session = JSON.parse(raw);
    state.accessToken = session.accessToken || "";
    const payload = decodeJWTPayload(state.accessToken);
    state.adminID = Number(session.adminID || payload.admin_id || 0);
    state.adminName = session.adminName || payload.sub || "";
    state.loginOpen = !state.accessToken;
  } catch (_error) {
    window.sessionStorage.removeItem(storageKey);
  }
}

function clearSession() {
  state.accessToken = "";
  state.adminID = 0;
  state.adminName = "";
  state.loginOpen = true;
  state.loginError = "";
  state.loading = false;
  state.data = { apps: null, logs: null };
  state.appForm = null;
  state.appFormError = "";
  state.appSecretReveal = null;
  persistSession();
}

function setFeedback(message, kind = "") {
  state.feedback = message;
  state.feedbackKind = kind;
}

function formatTime(value) {
  if (!value) {
    return "--";
  }
  const raw = String(value);
  const parsed = new Date(raw);
  if (!Number.isNaN(parsed.getTime())) {
    const pad = (number) => String(number).padStart(2, "0");
    return `${parsed.getFullYear()}-${pad(parsed.getMonth() + 1)}-${pad(parsed.getDate())} ${pad(parsed.getHours())}:${pad(parsed.getMinutes())}:${pad(parsed.getSeconds())}`;
  }
  const matched = raw.match(/^(\d{4}-\d{2}-\d{2})[T ](\d{2}:\d{2}:\d{2})/);
  return matched ? `${matched[1]} ${matched[2]}` : raw;
}

function maskSecret(value) {
  const raw = String(value || "");
  if (raw.length <= 10) {
    return raw || "--";
  }
  return `${raw.slice(0, 6)}******${raw.slice(-4)}`;
}

function generateAppSecret() {
  const bytes = new Uint8Array(24);
  window.crypto.getRandomValues(bytes);
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
}

async function copySecretText(value) {
  const text = String(value || "").trim();
  if (!text) {
    return false;
  }
  if (window.navigator.clipboard?.writeText) {
    await window.navigator.clipboard.writeText(text);
    return true;
  }

  const temp = document.createElement("textarea");
  temp.value = text;
  temp.setAttribute("readonly", "true");
  temp.style.position = "fixed";
  temp.style.opacity = "0";
  document.body.appendChild(temp);
  temp.select();
  let copied = false;
  try {
    copied = document.execCommand("copy");
  } catch (_error) {
    copied = false;
  } finally {
    document.body.removeChild(temp);
  }
  return copied;
}

function formatJSON(value) {
  if (value == null || value === "") {
    return "--";
  }
  if (typeof value === "string") {
    try {
      return JSON.stringify(JSON.parse(value), null, 2);
    } catch (_error) {
      return value;
    }
  }
  return JSON.stringify(value, null, 2);
}

async function requestAdminData(path, options = {}) {
  const headers = {
    Authorization: `Bearer ${state.accessToken}`,
    ...(options.headers || {})
  };
  const requestOptions = {
    method: options.method || "GET",
    headers
  };

  if (Object.prototype.hasOwnProperty.call(options, "json")) {
    requestOptions.body = JSON.stringify(options.json);
    requestOptions.headers["Content-Type"] = "application/json";
  } else if (Object.prototype.hasOwnProperty.call(options, "body")) {
    requestOptions.body = options.body;
  }

  let response;
  try {
    response = await window.fetch(path, requestOptions);
  } catch (_error) {
    throw new Error("连接服务失败，请确认 sar-admin 服务已启动，并刷新页面后重试。");
  }
  const payload = await response.json().catch(() => null);
  if (response.status === 401) {
    clearSession();
    renderWorkspace();
  }
  if (!response.ok || !payload || payload.status !== 200) {
    throw new Error(payload?.message || "请求失败");
  }
  return payload.data;
}

function getQueryString(view) {
  const params = new URLSearchParams();
  params.set("page", String(state.page[view] || 1));
  params.set("page_size", String(pageSize));
  Object.entries(state.filters[view] || {}).forEach(([key, value]) => {
    if (value !== "" && value != null) {
      params.set(key, String(value));
    }
  });
  return params.toString();
}

async function loadActiveView(force = false) {
  const config = viewConfigs[state.activeView];
  if (!hasSession() || !config.endpoint) {
    return;
  }
  if (!force && state.data[state.activeView]) {
    return;
  }

  state.loading = true;
  setFeedback("加载中...");
  renderWorkspace();

  try {
    const data = await requestAdminData(`${config.endpoint}?${getQueryString(state.activeView)}`);
    state.data[state.activeView] = data;
    state.loading = false;
    setFeedback(data?.items?.length ? "" : "暂无数据。");
  } catch (error) {
    state.loading = false;
    setFeedback(error instanceof Error ? error.message : "请求失败", "error");
  }
  renderWorkspace();
}

function renderAuth() {
  const loggedIn = hasSession();
  const showLogin = !loggedIn && state.loginOpen;
  document.body.classList.toggle("auth-backdrop-open", showLogin);
  document.body.classList.toggle("auth-modal-open", showLogin);
  loginForm.classList.toggle("hidden", !showLogin);
  loginButton.classList.toggle("hidden", loggedIn || showLogin);
  sessionPanel.classList.toggle("hidden", !loggedIn);
  sessionName.textContent = state.adminName || "已登录";

  const loginError = document.getElementById("login-error");
  loginError.textContent = state.loginError;
  loginError.classList.toggle("hidden", state.loginError === "");
}

function renderMenu() {
  menuItems.forEach((item) => {
    item.classList.toggle("active", item.dataset.view === state.activeView);
  });
}

function renderHeader() {
  const config = viewConfigs[state.activeView];
  viewKicker.textContent = config.kicker;
  viewTitle.textContent = config.title;
  panelActions.innerHTML = "";

  if (!hasSession()) {
    return;
  }

  config.actions.forEach((action) => {
    const button = document.createElement("button");
    button.type = "button";
    button.className = `button ${action === "create" ? "primary" : "secondary"}`;
    button.dataset.action = action;
    button.textContent = action === "create" ? "新增应用" : "刷新";
    panelActions.appendChild(button);
  });
}

function renderFeedback() {
  panelFeedback.textContent = state.feedback;
  panelFeedback.classList.toggle("hidden", state.feedback === "");
  panelFeedback.classList.toggle("error", state.feedbackKind === "error");
}

function closeSecretRevealPanel() {
  state.appSecretReveal = null;
  renderWorkspace();
}

function renderSecretRevealPanel() {
  if (!secretRevealPanel) {
    return;
  }

  const reveal = hasSession() ? state.appSecretReveal : null;
  if (!reveal) {
    secretRevealPanel.classList.add("hidden");
    secretRevealPanel.innerHTML = "";
    return;
  }

  secretRevealPanel.classList.remove("hidden");
  secretRevealPanel.innerHTML = `
    <section class="secret-reveal-card">
      <div class="modal-head">
        <div>
          <p class="eyebrow">secret reveal</p>
          <h3>${escapeHTML(reveal.title)}</h3>
        </div>
        <button class="button ghost inline modal-close" type="button" id="close-secret-reveal" aria-label="关闭 Secret 展示">×</button>
      </div>
      <p class="secret-reveal-desc">${escapeHTML(reveal.description)}</p>
      <div class="secret-reveal-value">${escapeHTML(reveal.secret)}</div>
      <div class="form-actions">
        <button class="button secondary" type="button" id="copy-secret">复制 Secret</button>
        <button class="button ghost" type="button" id="close-secret-reveal-text">我已保存</button>
      </div>
    </section>
  `;

  document.getElementById("copy-secret")?.addEventListener("click", async () => {
    try {
      const copied = await copySecretText(reveal.secret);
      state.feedback = copied ? "应用 Secret 已复制。" : "复制失败，请手动保存 Secret。";
      state.feedbackKind = copied ? "" : "error";
      renderFeedback();
    } catch (_error) {
      state.feedback = "复制失败，请手动保存 Secret。";
      state.feedbackKind = "error";
      renderFeedback();
    }
  });
  document.getElementById("close-secret-reveal")?.addEventListener("click", closeSecretRevealPanel);
  document.getElementById("close-secret-reveal-text")?.addEventListener("click", closeSecretRevealPanel);
}

function renderFilters() {
  const config = viewConfigs[state.activeView];
  if (!hasSession() || !config.filters?.length) {
    filterBar.innerHTML = "";
    return;
  }
  const values = state.filters[state.activeView] || {};
  filterBar.innerHTML = `
    <form id="filter-form" class="filter-form">
      ${config.filters
        .map(
          (item) => `
            <label class="form-row">
              <span class="form-label">${escapeHTML(item.label)}</span>
              <input name="${escapeHTML(item.key)}" type="${escapeHTML(item.type)}" value="${escapeHTML(values[item.key] || "")}" placeholder="${escapeHTML(item.placeholder || "")}" />
            </label>
          `
        )
        .join("")}
      <button class="button secondary" type="submit">查询</button>
    </form>
  `;
}

function renderAppForm() {
  if (!hasSession() || !state.appForm) {
    formPanel.classList.add("hidden");
    formPanel.innerHTML = "";
    return;
  }

  const isEdit = state.appForm.mode === "edit";
  const draft = state.appForm.draft || {};
  formPanel.classList.remove("hidden");
  formPanel.innerHTML = `
    <form id="app-form">
      <div class="modal-head">
        <div>
          <p class="eyebrow">${isEdit ? "修改应用" : "新增应用"}</p>
          <h3>${isEdit ? `应用 ${escapeHTML(state.appForm.id)}` : "应用授权"}</h3>
        </div>
        <button class="button ghost inline" type="button" data-action="close-app-form" aria-label="关闭表单">×</button>
      </div>
      <div class="modal-error ${state.appFormError ? "" : "hidden"}" role="alert">${escapeHTML(state.appFormError)}</div>
      <div class="form-grid">
        <label class="form-row">
          <span class="form-label">应用名称</span>
          <input name="name" type="text" value="${escapeHTML(draft.name || "")}" required />
        </label>
        <label class="form-row">
          <span class="form-label">${isEdit ? "新密钥" : "密钥"}</span>
          <div class="secret-input-group">
            <input name="secret" type="text" value="${escapeHTML(draft.secret || "")}" placeholder="${isEdit ? "留空保持原密钥" : "留空后自动生成"}" spellcheck="false" autocomplete="off" />
            <button class="button ghost inline regenerate-secret" type="button" data-action="regenerate-app-secret" aria-label="重新生成 Secret" data-tooltip="重新生成 Secret">↻</button>
          </div>
        </label>
        <label class="form-row wide">
          <span class="form-label">备注</span>
          <textarea name="remark">${escapeHTML(draft.remark || "")}</textarea>
        </label>
      </div>
      <div class="form-actions">
        <button class="button primary" type="submit">${isEdit ? "保存" : "新增"}</button>
        <button class="button secondary" type="button" data-action="close-app-form">取消</button>
      </div>
    </form>
  `;
}

function renderTable() {
  if (!hasSession()) {
    panelContent.innerHTML = "";
    pagination.innerHTML = "";
    return;
  }
  if (state.activeView === "apps") {
    renderAppsTable();
    return;
  }
  if (state.activeView === "logs") {
    renderLogsTable();
    return;
  }
  if (state.activeView === "es") {
    renderESDebug();
    return;
  }
  renderRecDebug();
}

function renderAppsTable() {
  const data = state.data.apps;
  const items = data?.items || [];
  if (!items.length) {
    panelContent.innerHTML = "";
    renderPagination(data);
    return;
  }
  panelContent.innerHTML = `
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>ID</th>
            <th>名称</th>
            <th>Secret</th>
            <th>备注</th>
            <th>创建时间</th>
            <th>更新时间</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          ${items
            .map(
              (item) => `
                <tr>
                  <td>${escapeHTML(item.id)}</td>
                  <td>${escapeHTML(item.name)}</td>
                  <td class="secret" title="${escapeHTML(item.secret || "")}">${escapeHTML(maskSecret(item.secret))}</td>
                  <td>${escapeHTML(item.remark || "--")}</td>
                  <td>${escapeHTML(formatTime(item.create_time))}</td>
                  <td>${escapeHTML(formatTime(item.last_update_time))}</td>
                  <td>
                    <div class="row-actions">
                      <button class="button secondary inline" type="button" data-action="edit-app" data-id="${escapeHTML(item.id)}">编辑</button>
                      <button class="button danger inline" type="button" data-action="delete-app" data-id="${escapeHTML(item.id)}">删除</button>
                    </div>
                  </td>
                </tr>
              `
            )
            .join("")}
        </tbody>
      </table>
    </div>
  `;
  renderPagination(data);
}

function renderLogsTable() {
  const data = state.data.logs;
  const items = data?.items || [];
  if (!items.length) {
    panelContent.innerHTML = "";
    renderPagination(data);
    return;
  }
  panelContent.innerHTML = `
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>ID</th>
            <th>管理员</th>
            <th>分类</th>
            <th>类型</th>
            <th>内容</th>
            <th>时间</th>
          </tr>
        </thead>
        <tbody>
          ${items
            .map(
              (item) => `
                <tr>
                  <td>${escapeHTML(item.id)}</td>
                  <td>${escapeHTML(item.admin_id)}</td>
                  <td>${escapeHTML(item.cate)}</td>
                  <td>${escapeHTML(item.type)}</td>
                  <td><pre class="muted">${escapeHTML(formatJSON(item.content))}</pre></td>
                  <td>${escapeHTML(formatTime(item.create_time))}</td>
                </tr>
              `
            )
            .join("")}
        </tbody>
      </table>
    </div>
  `;
  renderPagination(data);
}

function renderPagination(data) {
  if (!data || !viewConfigs[state.activeView].endpoint) {
    pagination.innerHTML = "";
    return;
  }
  const page = Number(data.page || state.page[state.activeView] || 1);
  const total = Number(data.total || 0);
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  pagination.innerHTML = `
    <button class="button secondary inline" type="button" data-action="prev-page" ${page <= 1 ? "disabled" : ""}>上一页</button>
    <span>第 ${escapeHTML(page)} / ${escapeHTML(totalPages)} 页，共 ${escapeHTML(total)} 条</span>
    <button class="button secondary inline" type="button" data-action="next-page" ${page >= totalPages ? "disabled" : ""}>下一页</button>
  `;
}

function renderESDebug() {
  formPanel.classList.add("hidden");
  panelContent.innerHTML = `
    <div class="debug-layout">
      <form id="es-debug-form" class="form-panel">
        <label class="form-row">
          <span class="form-label">操作</span>
          <select name="mode">
            <option value="index" ${state.debug.esMode === "index" ? "selected" : ""}>索引信息</option>
            <option value="doc" ${state.debug.esMode === "doc" ? "selected" : ""}>文档查看</option>
            <option value="search" ${state.debug.esMode === "search" ? "selected" : ""}>查询</option>
          </select>
        </label>
        <label class="form-row">
          <span class="form-label">AppID</span>
          <input name="appid" type="number" min="1" required />
        </label>
        <label class="form-row">
          <span class="form-label">Item ID</span>
          <input name="item_id" type="text" />
        </label>
        <label class="form-row">
          <span class="form-label">Query DSL</span>
          <textarea name="query">{"query":{"match_all":{}},"size":10}</textarea>
        </label>
        <div class="form-actions">
          <button class="button primary" type="submit">执行</button>
        </div>
      </form>
      <pre class="debug-result">${escapeHTML(state.debug.esResult)}</pre>
    </div>
  `;
  pagination.innerHTML = "";
}

function renderRecDebug() {
  formPanel.classList.add("hidden");
  panelContent.innerHTML = `
    <div class="debug-layout">
      <form id="rec-debug-form" class="form-panel">
        <div class="form-grid">
          <label class="form-row">
            <span class="form-label">类型</span>
            <select name="type">
              <option value="hot">hot</option>
              <option value="related">related</option>
              <option value="personalized">personalized</option>
              <option value="key">key</option>
            </select>
          </label>
          <label class="form-row">
            <span class="form-label">AppID</span>
            <input name="appid" type="text" required />
          </label>
          <label class="form-row">
            <span class="form-label">Item ID</span>
            <input name="item_id" type="text" />
          </label>
          <label class="form-row">
            <span class="form-label">User ID</span>
            <input name="user_id" type="text" />
          </label>
          <label class="form-row">
            <span class="form-label">Period</span>
            <input name="period" type="text" placeholder="hour / day / week" />
          </label>
          <label class="form-row">
            <span class="form-label">Size</span>
            <input name="size" type="number" min="1" value="20" />
          </label>
          <label class="form-row wide">
            <span class="form-label">Redis Key</span>
            <input name="key" type="text" />
          </label>
          <label class="form-row wide">
            <span class="form-label">排除 Item</span>
            <textarea name="exclude"></textarea>
          </label>
        </div>
        <div class="form-actions">
          <button class="button primary" type="submit">执行</button>
        </div>
      </form>
      <pre class="debug-result">${escapeHTML(state.debug.recResult)}</pre>
    </div>
  `;
  pagination.innerHTML = "";
}

function renderWorkspace() {
  renderAuth();
  renderMenu();
  renderHeader();
  renderFeedback();
  renderSecretRevealPanel();
  renderFilters();
  renderAppForm();
  renderTable();
}

function readFormData(form) {
  return Object.fromEntries(new FormData(form).entries());
}

function openAppForm(mode, appID = 0) {
  const item = (state.data.apps?.items || []).find((value) => Number(value.id) === Number(appID));
  state.appForm = {
    mode,
    id: appID,
    draft: {
      name: item?.name || "",
      remark: item?.remark || "",
      secret: mode === "create" ? generateAppSecret() : ""
    }
  };
  state.appFormError = "";
  state.appSecretReveal = null;
  renderWorkspace();
}

async function submitAppForm(form) {
  const values = readFormData(form);
  const isEdit = state.appForm?.mode === "edit";
  const payload = {
    name: String(values.name || "").trim(),
    remark: String(values.remark || "").trim()
  };
  const secret = String(values.secret || "").trim();
  if (secret || !isEdit) {
    payload.secret = secret;
  }
  if (!payload.name) {
    state.appFormError = "应用名称不能为空。";
    renderAppForm();
    return;
  }

  setFeedback(isEdit ? "正在保存..." : "正在新增...");
  renderFeedback();
  try {
    const responseData = await requestAdminData(isEdit ? `/api/v1/admin/app/${state.appForm.id}` : "/api/v1/admin/app", {
      method: isEdit ? "PUT" : "POST",
      json: payload
    });
    const revealSecret = !isEdit || secret !== "";
    state.appForm = null;
    state.appFormError = "";
    state.data.apps = null;
    state.appSecretReveal = revealSecret && responseData?.secret
      ? {
          title: isEdit ? "应用 Secret 已更新" : "应用 Secret 已创建",
          description: isEdit
            ? `应用 ${responseData.name || payload.name}（ID ${responseData.id}）的 Secret 已更新，请立即复制保存。`
            : `应用 ${responseData.name || payload.name}（ID ${responseData.id}）的 Secret 已创建，请立即复制保存。`,
          secret: responseData.secret
        }
      : null;
    setFeedback(isEdit ? "应用已保存。" : "应用已新增。");
    await loadActiveView(true);
  } catch (error) {
    state.appFormError = error instanceof Error ? error.message : "提交失败";
    renderAppForm();
  }
}

async function deleteApp(appID) {
  if (!window.confirm(`确认删除应用 ${appID}？`)) {
    return;
  }
  setFeedback("正在删除...");
  renderFeedback();
  try {
    await requestAdminData(`/api/v1/admin/app/${appID}`, { method: "DELETE" });
    state.data.apps = null;
    setFeedback("应用已删除。");
    await loadActiveView(true);
  } catch (error) {
    setFeedback(error instanceof Error ? error.message : "删除失败", "error");
    renderWorkspace();
  }
}

async function submitESDebug(form) {
  const values = readFormData(form);
  const mode = String(values.mode || "index");
  const appid = String(values.appid || "").trim();
  const itemID = String(values.item_id || "").trim();
  state.debug.esMode = mode;
  if (!appid) {
    state.debug.esResult = "AppID 不能为空。";
    renderWorkspace();
    return;
  }
  if (mode === "doc" && !itemID) {
    state.debug.esResult = "Item ID 不能为空。";
    renderWorkspace();
    return;
  }

  state.debug.esResult = "查询中...";
  renderWorkspace();
  try {
    let data;
    if (mode === "index") {
      data = await requestAdminData(`/api/v1/admin/debug/es/index/${encodeURIComponent(appid)}`);
    } else if (mode === "doc") {
      data = await requestAdminData(`/api/v1/admin/debug/es/doc/${encodeURIComponent(appid)}/${encodeURIComponent(itemID)}`);
    } else {
      data = await requestAdminData(`/api/v1/admin/debug/es/search/${encodeURIComponent(appid)}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: String(values.query || "{}")
      });
    }
    state.debug.esResult = JSON.stringify(data, null, 2);
  } catch (error) {
    state.debug.esResult = error instanceof Error ? error.message : "查询失败";
  }
  renderWorkspace();
}

async function submitRecDebug(form) {
  const values = readFormData(form);
  const payload = {
    type: String(values.type || "").trim(),
    appid: String(values.appid || "").trim(),
    item_id: String(values.item_id || "").trim(),
    user_id: String(values.user_id || "").trim(),
    period: String(values.period || "").trim(),
    key: String(values.key || "").trim(),
    size: Number(values.size || 20),
    exclude: String(values.exclude || "")
      .split(/[\n,]/)
      .map((item) => item.trim())
      .filter(Boolean)
  };
  if (!payload.type || !payload.appid) {
    state.debug.recResult = "类型和 AppID 不能为空。";
    renderWorkspace();
    return;
  }

  state.debug.recResult = "查询中...";
  renderWorkspace();
  try {
    const data = await requestAdminData("/api/v1/admin/debug/rec", {
      method: "POST",
      json: payload
    });
    state.debug.recResult = JSON.stringify(data, null, 2);
  } catch (error) {
    state.debug.recResult = error instanceof Error ? error.message : "查询失败";
  }
  renderWorkspace();
}

function bindAuthEvents() {
  document.getElementById("close-login-modal").addEventListener("click", () => {
    state.loginOpen = false;
    state.loginError = "";
    setFeedback("请先登录后查看数据。");
    renderWorkspace();
  });

  loginButton.addEventListener("click", () => {
    state.loginOpen = true;
    state.loginError = "";
    renderWorkspace();
    loginNameInput.focus();
  });

  loginForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    const name = loginNameInput.value.trim();
    const password = loginPasswordInput.value.trim();
    if (!name || !password) {
      state.loginError = "请输入管理员账号和密码。";
      renderAuth();
      return;
    }

    state.loginError = "";
    setFeedback("正在登录...");
    renderWorkspace();
    try {
      const response = await window.fetch("/api/v1/admin/auth/login", {
        method: "POST",
        headers: {
          "Content-Type": "application/json"
        },
        body: JSON.stringify({ name, password })
      });
      const payload = await response.json().catch(() => null);
      if (!response.ok || !payload || payload.status !== 200) {
        throw new Error(payload?.message || "登录失败");
      }

      state.accessToken = payload.data?.access_token || "";
      const jwtPayload = decodeJWTPayload(state.accessToken);
      state.adminID = Number(jwtPayload.admin_id || 0);
      state.adminName = jwtPayload.sub || name;
      state.loginOpen = false;
      loginPasswordInput.value = "";
      persistSession();
      setFeedback("登录成功。");
      renderWorkspace();
      loadActiveView(true);
    } catch (error) {
      clearSession();
      state.loginError = error instanceof Error ? error.message : "登录失败";
      setFeedback("", "error");
      renderWorkspace();
    }
  });

  loginForm.addEventListener("input", () => {
    if (!state.loginError) {
      return;
    }
    state.loginError = "";
    renderAuth();
  });

  logoutButton.addEventListener("click", async () => {
    try {
      await requestAdminData("/api/v1/admin/auth/logout", { method: "POST" });
      setFeedback("已退出登录。");
    } catch (_error) {
      setFeedback("已清理本地会话。");
    }
    clearSession();
    renderWorkspace();
  });
}

function bindWorkspaceEvents() {
  menuItems.forEach((item) => {
    item.addEventListener("click", () => {
      state.activeView = item.dataset.view;
      state.appForm = null;
      setFeedback(hasSession() ? "" : "请先登录后查看数据。");
      renderWorkspace();
      loadActiveView(false);
    });
  });

  panelActions.addEventListener("click", (event) => {
    const button = event.target.closest("button[data-action]");
    if (!button) {
      return;
    }
    if (button.dataset.action === "refresh") {
      state.data[state.activeView] = null;
      loadActiveView(true);
    }
    if (button.dataset.action === "create") {
      openAppForm("create");
    }
  });

  filterBar.addEventListener("submit", (event) => {
    event.preventDefault();
    const form = event.target.closest("form");
    if (!form) {
      return;
    }
    state.filters[state.activeView] = readFormData(form);
    state.page[state.activeView] = 1;
    state.data[state.activeView] = null;
    loadActiveView(true);
  });

  formPanel.addEventListener("input", (event) => {
    if (!state.appForm) {
      return;
    }
    const target = event.target;
    if (!target || !target.name) {
      return;
    }
    if (!Object.prototype.hasOwnProperty.call(state.appForm.draft || {}, target.name)) {
      return;
    }
    state.appForm.draft[target.name] = target.value;
  });

  formPanel.addEventListener("click", (event) => {
    const regenerate = event.target.closest("button[data-action='regenerate-app-secret']");
    if (regenerate) {
      if (!state.appForm) {
        return;
      }
      state.appForm.draft.secret = generateAppSecret();
      renderWorkspace();
      return;
    }
    const button = event.target.closest("button[data-action='close-app-form']");
    if (!button) {
      return;
    }
    state.appForm = null;
    state.appFormError = "";
    renderWorkspace();
  });

  formPanel.addEventListener("submit", (event) => {
    event.preventDefault();
    if (event.target.id === "app-form") {
      submitAppForm(event.target);
    }
  });

  panelContent.addEventListener("click", (event) => {
    const button = event.target.closest("button[data-action]");
    if (!button) {
      return;
    }
    const appID = Number(button.dataset.id || 0);
    if (button.dataset.action === "edit-app") {
      openAppForm("edit", appID);
    }
    if (button.dataset.action === "delete-app") {
      deleteApp(appID);
    }
  });

  panelContent.addEventListener("submit", (event) => {
    event.preventDefault();
    if (event.target.id === "es-debug-form") {
      submitESDebug(event.target);
    }
    if (event.target.id === "rec-debug-form") {
      submitRecDebug(event.target);
    }
  });

  pagination.addEventListener("click", (event) => {
    const button = event.target.closest("button[data-action]");
    if (!button || button.disabled) {
      return;
    }
    const view = state.activeView;
    const current = Number(state.page[view] || 1);
    state.page[view] = button.dataset.action === "next-page" ? current + 1 : Math.max(1, current - 1);
    state.data[view] = null;
    loadActiveView(true);
  });
}

function init() {
  restoreSession();
  bindAuthEvents();
  bindWorkspaceEvents();
  renderWorkspace();
  if (hasSession()) {
    loadActiveView(false);
  }
}

init();
