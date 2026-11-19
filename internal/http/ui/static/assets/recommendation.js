/* Immutable recommendation configuration. API calls use the existing admin JWT. */
const recommendationState = {appid: "", apps: [], sources: [], workspace: null, loaded: false, loading: false, error: "", tab: "experiments", draft: null, request: 0};
function recommendationReset() {
  Object.assign(recommendationState, {appid: "", apps: [], sources: [], workspace: null, loaded: false, loading: false, error: "", draft: null, request: recommendationState.request + 1});
}
const recEscape = value => escapeHTML(value);
const recType = value => value.recommendation_type;
const recTypeLabel = value => ({personalized: "个性化推荐", related: "相关推荐"})[value || "personalized"] || value;
const recEndpoint = suffix => `/api/v1/admin/recommendation/apps/${encodeURIComponent(recommendationState.appid)}${suffix}`;
const recItemProducers = [
  ["icf", "混合 Item CF", "icf"],
  ["click_icf", "点击 Item CF", "ckicf"],
  ["like_icf", "喜欢 Item CF", "like_icf"],
  ["comshare_icf", "评论和分享 Item CF", "comshare_icf"],
  ["subscribe_icf", "追更 Item CF", "subscribe_icf"],
  ["tipby_icf", "打赏和购买 Item CF", "tipby_icf"],
  ["ir", "IR", "ir"],
  ["nl", "Nearline", "nl"],
];
function recItemKeyTemplate(producer) {
  const entry = recItemProducers.find(([id]) => id === producer);
  return entry ? `${entry[2]}_{appid}_{itemid}` : "";
}
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
    const sources = await requestAdminData("/api/v1/admin/recommendation/recalls");
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
  const tabs = [["experiments", "实验"], ["strategies", "应用策略"], ["sources", "全局召回源"]];
  const navigation = `<div class="experiment-toolbar" role="tablist" aria-label="推荐配置">${tabs.map(([key, title]) => `<button type="button" class="button ${r.tab === key ? "primary" : "secondary"}" role="tab" aria-selected="${r.tab === key}" data-rec-action="tab" data-rec-value="${key}">${title}</button>`).join("")}</div>`;
  const errors = r.error ? `<div class="statistics-notice error" role="alert">${recEscape(r.error)}</div>` : "";
  if (r.draft) { panelContent.innerHTML = navigation + errors + renderRecommendationForm(); return; }
  let body = "";
  if (r.tab === "sources") {
    body = `<div class="experiment-toolbar"><p>全局召回源定义数据的读取方式。定义仅支持新增和删除，ID 不可重复使用。</p>${recButton("新增召回源", "new-source", "", true)}</div>`;
    body += recTable(["名称 / 标识", "读取类型 / 产出方", "Key 模板", "创建时间", "操作"], r.sources.map(s => `<tr><td>${recEscape(s.name)}<br><code>${recEscape(s.recall_id)}</code></td><td>${recEscape(s.kind)} / ${recEscape(s.producer)}</td><td><code>${recEscape(s.key_template)}</code></td><td>${formatTime(s.created_at)}</td><td>${recButton("删除", "delete-source", s.recall_id)}</td></tr>`), "先创建 Item CF、IR、Nearline、热门或外部召回源，再为应用配置策略。");
  } else if (!w) {
    body = `<div class="statistics-empty">请先在应用授权中创建应用，再配置推荐策略与实验。</div>`;
  } else if (r.tab === "strategies") {
    body = `<div class="experiment-toolbar"><p>个性化推荐支持召回、排序和兜底；相关推荐只执行召回和排序。两类策略都可在召回中添加最新上架，配置比例和权重。热门接口不参与策略分配。</p>${recButton("新建策略", "new-strategy", "", true)}</div>`;
    body += recTable(["名称 / ID", "类型", "召回配置", "兜底配置", "排序", "创建时间", "操作"], w.strategies.map(s => `<tr><td>${recEscape(s.name)}<br><code>${recEscape(s.strategy_id)}</code></td><td>${recEscape(recTypeLabel(recType(s)))}</td><td>${s.recall.map(v => `${recEscape(v.recall_id)} · 权重 ${v.weight} · ${v.recall_rate * 100}%${v.disabled ? "（停用）" : ""}`).join("<br>")}</td><td>${(s.fallback || []).map(v => `${recEscape(v.recall_id)} · 权重 ${v.weight} · ${v.recall_rate * 100}%${v.disabled ? "（停用）" : ""}`).join("<br>") || "—"}</td><td>${recEscape(s.rank)}</td><td>${formatTime(s.created_at)}</td><td>${recButton("删除", "delete-strategy", s.strategy_id)}</td></tr>`), "暂无策略，先选择召回源并设置权重和召回比例。");
    body += `<h4>策略使用历史</h4>` + recTable(["策略", "类型", "关联实验", "策略创建时间", "生效时间", "结束时间"], (w.strategy_history || []).map(h => `<tr><td>${recEscape(h.strategy_id)}</td><td>${recEscape(recTypeLabel(h.recommendation_type))}</td><td>${recEscape(h.experiment_id)}</td><td>${formatTime(h.created_at)}</td><td>${formatTime(h.started_at)}</td><td>${formatTime(h.ended_at)}</td></tr>`), "策略发布后记录生效区间；同一策略被多次使用时，分别保留历史。");
  } else {
    const p = w.publication, running = w.experiments.find(e => e.status === "running");
    body = `<div class="experiment-status"><div><strong>${running ? `${recEscape(running.name)}` : "当前使用默认推荐配置"}</strong><p>${p.pending ? "待同步，后台自动重试" : "已同步"}</p></div>${running ? recButton("停止实验", "stop") : ""}</div><div class="experiment-toolbar"><p>一个应用同时运行一个实验，两类接口分别分配流量。每次调整都使用新 ID 创建定义；仅已配置该类型策略的接口参与实验，其剩余流量进入 default。</p>${recButton("创建实验", "new-experiment", "", true)}</div>`;
    body += recTable(["实验", "分桶与策略", "状态", "创建时间", "生效时间", "结束时间", "操作"], w.experiments.map((e, index) => `<tr><td>${recEscape(e.name)}<br><code>${recEscape(e.experiment_id)}</code></td><td>${recEscape(e.bucket_by)} / SHA1<br>${e.assignments.map(a => `${recEscape(recTypeLabel(recType(e.snapshot.strategies.find(s => s.strategy_id === a.strategy_id) || {})))} · ${recEscape(a.strategy_id)}：${a.bucket_start}–${a.bucket_end}（${a.bucket_end-a.bucket_start+1}%）`).join("<br>")}</td><td>${({draft: "草稿", running: "运行中", ended: "已结束"})[e.status]}</td><td>${formatTime(e.created_at)}</td><td>${formatTime(e.started_at)}</td><td>${formatTime(e.ended_at)}</td><td><div class="experiment-row-actions">${e.status === "draft" ? recButton("发布", "publish", index, true) : ""}${e.status!=="running" ? recButton("删除", "delete-experiment", e.experiment_id) : ""}${recButton("配置快照", "snapshot", index)}</div></td></tr>`), "暂无实验，先创建策略，再创建实验并分配流量。");
  }
  panelContent.innerHTML = navigation + errors + body;
}
function recInput(name, label, value = "", attrs = "") {
  return `<label class="form-row"><span class="form-label">${label}</span><input name="${name}" value="${recEscape(value)}" ${attrs}></label>`;
}
function recSelect(name, label, values, value) {
  return `<label class="form-row"><span class="form-label">${label}</span><select name="${name}">${values.map(([key, text]) => `<option value="${recEscape(key)}" ${key === value ? "selected" : ""}>${recEscape(text)}</option>`).join("")}</select></label>`;
}
function recExperimentAssignments(form, values) {
  return [...form.querySelectorAll('input[name="strategy"]:checked')].map(i => ({strategy_id:i.value,bucket_start:Number(values[`start_${i.value}`]),bucket_end:Number(values[`start_${i.value}`])+Number(values[`percent_${i.value}`])-1}));
}
function recExperimentFlow(v,type) {
  const strategies=(recommendationState.workspace?.strategies || []).filter(s=>recType(s)===type);
  return `<fieldset class="experiment-fieldset"><legend>${recTypeLabel(type)}流量</legend>${strategies.map(s=>`<div class="experiment-recall experiment-options"><label><input type="checkbox" name="strategy" value="${recEscape(s.strategy_id)}"> ${recEscape(s.name)}（${recEscape(s.strategy_id)}）</label><label>起始桶 <input aria-label="${recEscape(s.name)}起始桶" type="number" name="start_${s.strategy_id}" min="0" max="99" step="1" value="0"></label><label>流量 % <input aria-label="${recEscape(s.name)}流量百分比" type="number" name="percent_${s.strategy_id}" min="1" max="100" step="1" value="29"></label></div>`).join("") || "请先创建该类型的策略。"}</fieldset>`;
}
function recRouteFields(stage,label,routes,recalls,inactive=false) {
 return `<fieldset class="experiment-fieldset" ${stage==="fallback" ? 'data-rec-personalized' : ''} ${inactive ? 'hidden disabled' : ''}><legend>${label}</legend>${recalls.map(s=>{
  const route=routes.find(r=>r.recall_id===s.recall_id), id=s.recall_id, prefix=`${stage}_${id}`;
  return `<div class="experiment-recall experiment-options"><label><input type="checkbox" name="${stage}" value="${recEscape(id)}" ${route?'checked':''}> ${recEscape(s.name)}</label><label>状态 <select name="disabled_${prefix}" aria-label="${recEscape(s.name)}${label}状态"><option value="false" ${!route?.disabled?'selected':''}>启用</option><option value="true" ${route?.disabled?'selected':''}>停用</option></select></label><label>权重 <input aria-label="${recEscape(s.name)}${label}权重" type="number" name="weight_${prefix}" min="0.001" max="1000" step="any" value="${route?.weight??1}"></label><label>召回 % <input aria-label="${recEscape(s.name)}${label}比例" type="number" name="rate_${prefix}" min="0.001" max="100" step="any" value="${(route?.recall_rate??1)*100}"></label></div>`;
 }).join('') || '请先新增召回源。'}</fieldset>`;
}
function recReadRoutes(form,values,stage) {
 return [...form.querySelectorAll(`input[name="${stage}"]:checked`)].map(i=>({recall_id:i.value,weight:Number(values[`weight_${stage}_${i.value}`]),recall_rate:Number(values[`rate_${stage}_${i.value}`])/100,disabled:values[`disabled_${stage}_${i.value}`]==="true"}));
}
function renderRecommendationForm() {
  const r = recommendationState, {kind, value: v} = r.draft;
  if (kind === "snapshot") return `<div class="experiment-toolbar"><h4>历史配置快照</h4>${recButton("返回", "cancel")}</div><pre class="experiment-snapshot">${recEscape(JSON.stringify(v, null, 2))}</pre>`;
  let content = "", title = "";
  if (kind === "source") {
    title = "新增全局召回源";
    const producer = v.producer || "icf";
    const keyTemplate = producer === "hot" ? `hot_{appid}_${v.hot_period || "hour"}` : (v.key_template || recItemKeyTemplate(producer));
    const producers = [...recItemProducers, ["hot", "榜单（热门 / 上新）"], ["external", "外部服务（含 User CF）"]];
    content = recInput("name", "名称", v.name, "required maxlength=64")
      + recInput("recall_id", "标识", v.recall_id, 'required pattern="[a-z][a-z0-9_]{0,47}" maxlength=48')
      + recSelect("kind", "读取类型", [["item", "按物品"], ["user", "按用户"], ["global", "全局榜单"]], v.kind || "item")
      + recSelect("producer", "数据产出", producers, producer)
      + recInput("key_template", "Key 模板", keyTemplate, `required maxlength=256 ${producer === "hot" ? "readonly" : ""}`);
    content += recSelect("hot_period", "榜单类型（仅榜单产出使用）", [["hour","小时"],["day","日榜"],["week","周榜"],["month","月榜"],["quarter","季榜"],["all","总榜"],["newest","上新（nearline）"]], v.hot_period || "hour");
    content += `<p class="experiment-help">选择内置产出方时会填入默认 Key 模板。{appid} 必须单独占一段，物品和用户召回分别使用 {itemid} 和 {userid}。上新榜由 Nearline 维护。</p>`;
  } else if (kind === "strategy") {
    title = "新建应用策略";
    content = recSelect("recommendation_type", "推荐类型", [["personalized", "个性化推荐"], ["related", "相关推荐"]], recType(v)) + recInput("name", "策略中文名称", v.name, "required maxlength=64") + recInput("strategy_id", "策略 ID", v.strategy_id, `required pattern="[a-z][a-z0-9_]{0,47}" maxlength=48 `);
    content += recRouteFields("recall","召回",v.recall || [],r.sources);
    content += recSelect("rank","排序",[["recall_weight","按召回权重排序"]],v.rank || "recall_weight");
    content += recRouteFields("fallback","兜底",v.fallback || [],r.sources.filter(s=>s.kind==="global"),recType(v)==="related");
    content += `<p class="experiment-help">召回和兜底使用相同格式。兜底按缺口计算各来源配额，合并排序后补齐。相关推荐只使用召回和排序。最新上架作为普通召回添加，否则新品可能无法推荐。</p>`;
  } else {
    title = "创建实验";
    content = recInput("name","实验名称",v.name,"required maxlength=64") + recInput("experiment_id","实验 ID",v.experiment_id,'required pattern="[a-z][a-z0-9_]{0,47}" maxlength=48') + recSelect("bucket_by","分桶身份",[["user_id","用户 ID"],["anonymous_id","匿名 ID"]],v.bucket_by || "user_id");
    content += recExperimentFlow(v,"personalized") + recExperimentFlow(v,"related") + `<p class="experiment-help">固定使用身份的 SHA1 前 8 字节大端整数对 100 取余。两种类型分别分配桶；未命中的流量执行 Online 当前 default，未参与类型和缺失身份不记入实验。</p>`;
  }
  return `<form id="recommendation-form" class="experiment-form"><h4>${title}</h4><div class="experiment-grid">${content}</div><div class="experiment-toolbar"><button class="button primary" type="submit">创建定义</button>${recButton("取消", "cancel")}</div></form>`;
}
async function saveRecommendationForm(form) {
  const r = recommendationState, d = r.draft, values = readFormData(form);
  let endpoint, body;
  if (d.kind === "source") {
    endpoint = "/api/v1/admin/recommendation/recalls";
    body = {recall_id: values.recall_id, name: values.name, kind: values.kind, producer: values.producer, hot_period: values.producer === "hot" ? values.hot_period : "", key_template: values.key_template, value_format: "compact_score"};
  } else if (d.kind === "strategy") {
    endpoint = recEndpoint("/strategies");
    body = {strategy_id:values.strategy_id,name:values.name,recommendation_type:values.recommendation_type,recall:recReadRoutes(form,values,"recall"),fallback:values.recommendation_type==="personalized" ? recReadRoutes(form,values,"fallback") : [],rank:values.rank};
  } else {
    endpoint = recEndpoint("/experiments");
    const assignments = recExperimentAssignments(form, values);
    body = {experiment_id: values.experiment_id, name: values.name, bucket_by: values.bucket_by, assignments};
  }
  const button = form.querySelector('button[type="submit"]'); button.disabled = true;
  try {
    await requestAdminData(endpoint, {method: "POST", json: body});
    r.draft = null; await loadRecommendation(true);
  } catch (error) { r.error = error.message || "保存失败"; let alert = form.querySelector('[role="alert"]'); if (!alert) {alert = document.createElement("p");alert.setAttribute("role","alert");alert.className="statistics-notice error";form.prepend(alert);} alert.textContent = r.error; button.disabled = false; }
}
function bindRecommendationEvents() {
  panelContent.addEventListener("change", event => {
    const form = event.target.form;
    if (form?.id !== "recommendation-form") return;
    if (event.target.name === "producer" || event.target.name === "hot_period") {
      const producer = form.elements.namedItem("producer").value;
      const key = form.elements.namedItem("key_template");
      key.readOnly = producer === "hot";
      if (producer === "hot") {
        form.elements.namedItem("kind").value = "global";
        key.value = `hot_{appid}_${form.elements.namedItem("hot_period").value}`;
      } else if (event.target.name === "producer") {
        const template = recItemKeyTemplate(producer);
        if (template) {
          form.elements.namedItem("kind").value = "item";
          key.value = template;
        }
      }
    }
    if (event.target.name === "recommendation_type") {
      const fields = form.querySelector("[data-rec-personalized]");
      fields.hidden = event.target.value === "related";
      fields.disabled = fields.hidden;
    }
  });
  panelContent.addEventListener("submit", event => {if (event.target.id === "recommendation-form") {event.preventDefault();saveRecommendationForm(event.target);}});
  panelContent.addEventListener("click", async event => {
    const button = event.target.closest("button[data-rec-action]"); if (!button || state.activeView !== "experiments") return;
    const r = recommendationState, action = button.dataset.recAction, value = button.dataset.recValue;
    r.error = "";
    if (action === "tab") {r.tab = value;r.draft = null;}
    if (action === "cancel") r.draft = null;
    if (action === "new-source") r.draft = {kind: "source", value: {}};
    if (action === "new-strategy") r.draft = {kind: "strategy", value: {}};
    if (action === "new-experiment") r.draft = {kind: "experiment", value: {}};
    if (action.startsWith("delete-")) {
      const kind=action.slice(7), path=kind==="source" ? `/api/v1/admin/recommendation/recalls/${encodeURIComponent(value)}` : recEndpoint(`/${kind==="strategy"?"strategies":"experiments"}/${encodeURIComponent(value)}`);
      button.disabled=true;
      try { await requestAdminData(path,{method:"DELETE"}); await loadRecommendation(true); } catch(error) { r.error=error.message || "删除失败，定义可能仍被引用"; }
    }
    if (action === "snapshot") r.draft = {kind: "snapshot", value: r.workspace.experiments[Number(value)]};
    if (action === "publish" || action === "stop") {
      button.disabled = true;
      const e = action === "publish" ? r.workspace.experiments[Number(value)] : null;
      try { await requestAdminData(recEndpoint("/publication"), {method: "POST", json: {experiment_id: e?.experiment_id || ""}}); await loadRecommendation(true); }
      catch (error) {r.error = error.message || "发布失败";}
    }
    renderWorkspace();
  });
}
