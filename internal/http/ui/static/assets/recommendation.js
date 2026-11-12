/* Versioned recommendation configuration. API calls use the existing admin JWT. */
const recommendationState = {appid: "", apps: [], sources: [], workspace: null, loaded: false, loading: false, error: "", tab: "experiments", draft: null, request: 0};
function recommendationReset() {
  Object.assign(recommendationState, {appid: "", apps: [], sources: [], workspace: null, loaded: false, loading: false, error: "", draft: null, request: recommendationState.request + 1});
}
const recEscape = value => escapeHTML(value);
const recEndpoint = suffix => `/api/v1/admin/recommendation/apps/${encodeURIComponent(recommendationState.appid)}${suffix}`;
async function loadRecommendation(force = false) {
  const r = recommendationState;
  if (!hasSession() || (r.loading && !force) || (r.loaded && !force)) return;
  const request = ++r.request, session = state.accessToken;
  r.loading = true; r.error = ""; setFeedback(""); renderWorkspace();
  try {
    const apps = [];
    for (let page = 1; ; page++) {
      const data = await requestAdminData(`/api/v1/admin/app?page=${page}&page_size=100`);
      apps.push(...data.items);
      if (!data.items.length || apps.length >= data.total) break;
    }
    if (request !== r.request || session !== state.accessToken) return;
    r.apps = apps;
    if (!apps.some(a => String(a.id) === r.appid)) r.appid = String(apps[0]?.id || "");
    const sources = await requestAdminData("/api/v1/admin/recommendation/sources");
    const workspace = r.appid ? await requestAdminData(recEndpoint("")) : null;
    if (request !== r.request || session !== state.accessToken) return;
    r.sources = sources; r.workspace = workspace; r.loaded = true;
  } catch (error) {
    if (request === r.request && session === state.accessToken) r.error = error.message || "配置加载失败";
  } finally {
    if (request === r.request) { r.loading = false; if (state.activeView === "experiments") renderWorkspace(); }
  }
}
function renderRecommendationFilters() {
  const r = recommendationState;
  filterBar.innerHTML = `<form class="filter-form"><label class="form-row"><span class="form-label">应用</span><select name="appid" aria-label="选择推荐应用">${r.apps.map(a => `<option value="${recEscape(a.id)}" ${String(a.id) === r.appid ? "selected" : ""}>${recEscape(a.name)} · ${recEscape(a.id)}</option>`).join("")}</select></label><button class="button secondary" type="submit" ${r.loading ? "disabled" : ""}>切换应用</button></form>`;
}
function recButton(label, action, value = "", primary = false) {
  return `<button type="button" class="button ${primary ? "primary" : "secondary"} inline" data-rec-action="${action}" data-rec-value="${recEscape(value)}" ${recommendationState.loading ? "disabled" : ""}>${label}</button>`;
}
function recTable(headers, rows, empty) {
  return rows.length ? `<div class="table-wrap"><table><thead><tr>${headers.map(h => `<th>${h}</th>`).join("")}</tr></thead><tbody>${rows.join("")}</tbody></table></div>` : `<div class="statistics-empty">${empty}</div>`;
}
function renderRecommendation() {
  const r = recommendationState, w = r.workspace;
  pagination.innerHTML = "";
  if (r.loading) { panelContent.innerHTML = `<div class="statistics-skeleton" role="status" aria-label="正在加载推荐配置"><div></div><div></div><div></div></div>`; return; }
  const tabs = [["experiments", "实验与版本"], ["strategies", "应用策略"], ["sources", "全局召回源"]];
  const navigation = `<div class="experiment-toolbar" role="tablist" aria-label="推荐配置">${tabs.map(([key, title]) => `<button type="button" class="button ${r.tab === key ? "primary" : "secondary"}" role="tab" aria-selected="${r.tab === key}" data-rec-action="tab" data-rec-value="${key}">${title}</button>`).join("")}</div>`;
  const errors = r.error ? `<div class="statistics-notice error" role="alert">${recEscape(r.error)}</div>` : "";
  if (r.draft) { panelContent.innerHTML = navigation + errors + renderRecommendationForm(); return; }
  let body = "";
  if (r.tab === "sources") {
    body = `<div class="experiment-toolbar"><p>全局召回源定义数据的读取方式。修改后保存为新版本，已发布实验保留原快照。</p>${recButton("新增召回源", "new-source", "", true)}</div>`;
    body += recTable(["名称 / 标识", "版本", "读取类型 / 产出方", "Key 模板", "创建时间", "操作"], r.sources.map(s => `<tr><td>${recEscape(s.name)}<br><code>${recEscape(s.source_id)}</code></td><td>v${s.version}</td><td>${recEscape(s.kind)} / ${recEscape(s.producer)}</td><td><code>${recEscape(s.key_template)}</code></td><td>${formatTime(s.created_at)}</td><td>${recButton("创建新版本", "edit-source", s.source_id)}</td></tr>`), "先创建 Item CF、IR、Nearline、热门或外部召回源，再为应用配置策略。");
  } else if (!w) {
    body = `<div class="statistics-empty">请先在应用授权中创建应用，再配置推荐策略与实验。</div>`;
  } else if (r.tab === "strategies") {
    body = `<div class="experiment-toolbar"><p>策略按应用管理。保存后，通过新的实验版本发布生效。</p>${recButton("新建策略", "new-strategy", "", true)}</div>`;
    body += recTable(["名称 / ID", "版本", "召回配置", "排序", "创建时间", "操作"], w.strategies.map(s => `<tr><td>${recEscape(s.name)}<br><code>${recEscape(s.strategy_id)}</code></td><td>v${s.version}</td><td>${s.recall.map(v => `${recEscape(v.source_id)} · 权重 ${v.weight} · ${(v.recall_rate * 100).toFixed(0)}%${v.disabled ? "（停用）" : ""}`).join("<br>")}</td><td>${recEscape(s.rank)}</td><td>${formatTime(s.created_at)}</td><td>${recButton("创建新版本", "edit-strategy", s.strategy_id)}</td></tr>`), "暂无策略，先选择召回源并设置权重和召回比例。");
    body += `<h4>策略使用历史</h4>` + recTable(["策略 / 版本", "关联实验 / 版本", "策略创建时间", "生效时间", "结束时间"], (w.strategy_history || []).map(h => `<tr><td>${recEscape(h.strategy_id)} · v${h.strategy_version}</td><td>${recEscape(h.experiment_id)} · v${h.experiment_version}</td><td>${formatTime(h.created_at)}</td><td>${formatTime(h.started_at)}</td><td>${formatTime(h.ended_at)}</td></tr>`), "策略发布后记录生效区间；同一策略被多次使用时，分别保留历史。");
  } else {
    const p = w.publication, running = w.experiments.find(e => e.status === "running");
    body = `<div class="experiment-status"><div><strong>${running ? `${recEscape(running.name)} · v${running.experiment_version}` : "当前使用默认推荐配置"}</strong><p>发布版本 ${p.published_revision} ${p.revision > p.published_revision ? `· 待发布 ${p.revision}，后台自动重试` : "· 已同步"}</p></div>${running ? recButton("停止实验", "stop") : ""}</div><div class="experiment-toolbar"><p>一个应用同时运行一个实验。每次调整都新建版本，剩余流量进入 default 对照组。</p>${recButton("创建实验", "new-experiment", "", true)}</div>`;
    body += recTable(["实验 / 版本", "分桶与策略", "状态", "创建时间", "生效时间", "结束时间", "操作"], w.experiments.map((e, index) => `<tr><td>${recEscape(e.name)}<br><code>${recEscape(e.experiment_id)}</code> · v${e.experiment_version}</td><td>${recEscape(e.bucket_by)} / ${recEscape(e.bucket_algorithm)}<br>${e.assignments.map(a => `${recEscape(a.strategy_id)} v${a.strategy_version}：${a.bucket_start}–${a.bucket_end}（${a.bucket_end-a.bucket_start+1}%）`).join("<br>")}</td><td>${({draft: "草稿", running: "运行中", ended: "已结束"})[e.status]}</td><td>${formatTime(e.created_at)}</td><td>${formatTime(e.started_at)}</td><td>${formatTime(e.ended_at)}</td><td><div class="experiment-row-actions">${e.status === "draft" ? recButton("发布", "publish", index, true) : ""}${recButton("创建新版本", "edit-experiment", index)}${recButton("配置快照", "snapshot", index)}</div></td></tr>`), "暂无实验，先创建策略，再创建实验并分配流量。");
  }
  panelContent.innerHTML = navigation + errors + body;
}
function recInput(name, label, value = "", attrs = "") {
  return `<label class="form-row"><span class="form-label">${label}</span><input name="${name}" value="${recEscape(value)}" ${attrs}></label>`;
}
function recSelect(name, label, values, value) {
  return `<label class="form-row"><span class="form-label">${label}</span><select name="${name}">${values.map(([key, text]) => `<option value="${recEscape(key)}" ${key === value ? "selected" : ""}>${recEscape(text)}</option>`).join("")}</select></label>`;
}
function renderRecommendationForm() {
  const r = recommendationState, {kind, value: v} = r.draft;
  if (kind === "snapshot") return `<div class="experiment-toolbar"><h4>历史配置快照</h4>${recButton("返回", "cancel")}</div><pre class="experiment-snapshot">${recEscape(JSON.stringify(v, null, 2))}</pre>`;
  let content = "", title = "";
  if (kind === "source") {
    title = `${v.version ? "创建召回源新版本" : "新增全局召回源"}`;
    content = recInput("name", "名称", v.name, "required maxlength=64") + recInput("source_id", "标识", v.source_id, `required pattern="[a-z][a-z0-9_]{0,47}" maxlength=48 ${v.version ? "readonly" : ""}`) + recSelect("kind", "读取类型", [["item", "按物品"], ["user", "按用户"], ["global", "全局榜单"]], v.kind || "item") + recSelect("producer", "数据产出", [["icf", "Item CF"], ["ir", "IR"], ["nl", "Nearline"], ["hot", "热门榜"], ["external", "外部服务（含 User CF）"]], v.producer || "icf") + recInput("key_template", "Key 模板", v.key_template || "icf_{appid}_{itemid}", "required maxlength=256");
    content += recSelect("hot_period", "热门源周期（仅热门榜使用）", [["hour","小时"],["day","天"],["week","周"],["month","月"],["quarter","季度"],["all","全部"],["newest","最新上架"]], v.hot_period || "hour");
    content += `<p class="experiment-help">模板支持 {appid}、{itemid}、{userid}，按读取类型校验。内置产出方的模板以 icf_、ir_、nl_、hot_ 开头；用户召回由外部服务产出。Value 使用现有紧凑分数格式。</p>`;
  } else if (kind === "strategy") {
    title = v.version ? "创建策略新版本" : "新建应用策略";
    content = recInput("name", "策略中文名称", v.name, "required maxlength=64") + recInput("strategy_id", "策略 ID", v.strategy_id, `required pattern="[a-z][a-z_]{0,9}" maxlength=10 ${v.version ? "readonly" : ""}`);
    content += `<fieldset class="experiment-fieldset"><legend>召回源、权重与召回比例</legend>${r.sources.map(s => { const route = v.recall?.find(a => a.source_id === s.source_id); return `<div class="experiment-recall"><label><input type="checkbox" name="source" value="${recEscape(s.source_id)}" ${route ? "checked" : ""}> ${recEscape(s.name)}</label><label>权重 <input type="number" name="weight_${recEscape(s.source_id)}" aria-label="${recEscape(s.name)}权重" min="0.01" max="1000" step="0.01" value="${route?.weight || 1}"></label><label>召回 % <input type="number" name="rate_${recEscape(s.source_id)}" aria-label="${recEscape(s.name)}召回比例" min="1" max="100" step="1" value="${Math.round((route?.recall_rate || 1)*100)}"></label></div>`; }).join("") || "请先创建全局召回源。"}</fieldset>`;
    const globals = [["", "不启用"], ...r.sources.filter(s => s.kind === "global").map(s => [s.source_id, s.name])];
    content += recSelect("fallback_source_id", "兜底榜单", globals, v.fallback_source_id || "") + recSelect("newest_source_id", "上新榜单", globals, v.newest?.source_id || "") + recInput("newest_percent", "上新占比 %", v.newest?.share_percent || 0, "type=number min=0 max=100 step=1") + recInput("newest_scenes", "上新场景（逗号分隔）", v.newest?.scenes?.join(",") || "", "maxlength=1024") + `<p class="experiment-help">排序使用 recall_weight。策略 ID 为 10 字符内的小写字母与下划线，default 保留给对照组。</p>`;
  } else {
    title = v.experiment_version ? "创建实验新版本" : "创建实验";
    content = recInput("name", "实验名称", v.name, "required maxlength=64") + recInput("experiment_id", "实验 ID", v.experiment_id, `required pattern="[a-z][a-z0-9_]{0,47}" maxlength=48 ${v.experiment_version ? "readonly" : ""}`) + recSelect("bucket_by", "分桶身份", [["user_id", "登录用户 ID"], ["anonymous_id", "匿名用户 ID"]], v.bucket_by || "user_id") + recInput("salt", "分桶盐值", v.salt || "", "maxlength=64");
    content += `<fieldset class="experiment-fieldset"><legend>策略流量</legend>${(r.workspace?.strategies || []).map(s => {const a = v.assignments?.find(a => a.strategy_id === s.strategy_id);return `<div class="experiment-recall"><label><input type="checkbox" name="strategy" value="${recEscape(s.strategy_id)}" ${a ? "checked" : ""}> ${recEscape(s.name)}（${recEscape(s.strategy_id)} v${s.version}）</label><label>起始桶 <input aria-label="${recEscape(s.name)}起始桶" type="number" name="start_${s.strategy_id}" min="0" max="99" step="1" value="${a?.bucket_start ?? 0}"></label><label>流量 % <input aria-label="${recEscape(s.name)}流量百分比" type="number" name="percent_${s.strategy_id}" min="1" max="99" step="1" value="${a ? a.bucket_end-a.bucket_start+1 : 29}"></label></div>`;}).join("") || "请先创建应用策略。"}</fieldset><p class="experiment-help">桶范围为 0–99，各策略范围不能重叠；未分配部分进入 default。数字和字符串 ID 统一使用稳定哈希分桶；同一实验和盐值下分桶结果不随版本变化。</p>`;
  }
  return `<form id="recommendation-form" class="experiment-form"><h4>${title}</h4><div class="experiment-grid">${content}</div><div class="experiment-toolbar"><button class="button primary" type="submit">保存新版本</button>${recButton("取消", "cancel")}</div></form>`;
}
async function saveRecommendationForm(form) {
  const r = recommendationState, d = r.draft, values = readFormData(form);
  let endpoint, body;
  if (d.kind === "source") {
    endpoint = "/api/v1/admin/recommendation/sources";
    body = {source_id: values.source_id, name: values.name, version: d.value.version || 0, kind: values.kind, producer: values.producer, hot_period: values.producer === "hot" ? values.hot_period : "", key_template: values.key_template, value_format: "compact_score"};
  } else if (d.kind === "strategy") {
    endpoint = recEndpoint("/strategies");
    const recall = [...form.querySelectorAll('input[name="source"]:checked')].map(i => ({source_id: i.value, weight: Number(values[`weight_${i.value}`]), recall_rate: Number(values[`rate_${i.value}`])/100, disabled: false}));
    body = {strategy_id: values.strategy_id, name: values.name, version: d.value.version || 0, recall, fallback_source_id: values.fallback_source_id, newest: {source_id: values.newest_source_id, share_percent: Number(values.newest_percent), scenes: values.newest_scenes.split(",").map(s => s.trim()).filter(Boolean)}, rank: "recall_weight"};
  } else {
    endpoint = recEndpoint("/experiments");
    const assignments = [...form.querySelectorAll('input[name="strategy"]:checked')].map(i => ({strategy_id: i.value, strategy_version: 0, bucket_start: Number(values[`start_${i.value}`]), bucket_end: Number(values[`start_${i.value}`])+Number(values[`percent_${i.value}`])-1}));
    body = {experiment_id: values.experiment_id, name: values.name, experiment_version: d.value.experiment_version || 0, bucket_by: values.bucket_by, bucket_algorithm: "sha256", salt: values.salt, assignments};
  }
  const button = form.querySelector('button[type="submit"]'); button.disabled = true;
  try {
    await requestAdminData(endpoint, {method: "POST", json: body});
    r.draft = null; await loadRecommendation(true);
  } catch (error) { r.error = error.message || "保存失败"; let alert = form.querySelector('[role="alert"]'); if (!alert) {alert = document.createElement("p");alert.setAttribute("role","alert");alert.className="statistics-notice error";form.prepend(alert);} alert.textContent = r.error; button.disabled = false; }
}
function bindRecommendationEvents() {
  panelContent.addEventListener("submit", event => {if (event.target.id === "recommendation-form") {event.preventDefault();saveRecommendationForm(event.target);}});
  panelContent.addEventListener("click", async event => {
    const button = event.target.closest("button[data-rec-action]"); if (!button || state.activeView !== "experiments") return;
    const r = recommendationState, action = button.dataset.recAction, value = button.dataset.recValue;
    r.error = "";
    if (action === "tab") {r.tab = value;r.draft = null;}
    if (action === "cancel") r.draft = null;
    if (action === "new-source") r.draft = {kind: "source", value: {}};
    if (action === "edit-source") r.draft = {kind: "source", value: structuredClone(r.sources.find(s => s.source_id === value))};
    if (action === "new-strategy") r.draft = {kind: "strategy", value: {}};
    if (action === "edit-strategy") r.draft = {kind: "strategy", value: structuredClone(r.workspace.strategies.find(s => s.strategy_id === value))};
    if (action === "new-experiment") r.draft = {kind: "experiment", value: {}};
    if (action === "edit-experiment") {
      const e = structuredClone(r.workspace.experiments[Number(value)]);
      e.experiment_version = Math.max(...r.workspace.experiments.filter(v => v.experiment_id === e.experiment_id).map(v => v.experiment_version));
      r.draft = {kind: "experiment", value: e};
    }
    if (action === "snapshot") r.draft = {kind: "snapshot", value: r.workspace.experiments[Number(value)]};
    if (action === "publish" || action === "stop") {
      button.disabled = true;
      const e = action === "publish" ? r.workspace.experiments[Number(value)] : null;
      try { await requestAdminData(recEndpoint("/publication"), {method: "POST", json: {experiment_id: e?.experiment_id || "", experiment_version: e?.experiment_version || 0, expected_revision: r.workspace.publication.revision}}); await loadRecommendation(true); }
      catch (error) {r.error = error.message || "发布失败";}
    }
    renderWorkspace();
  });
}
