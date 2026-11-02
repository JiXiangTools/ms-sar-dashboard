/* Business analytics UI. Data comes exclusively from authenticated admin APIs. */
const statisticsState = {
  apps: [], loadedApps: false, data: null, loading: false, error: "", request: 0,
  filters: { appid: "", from: "", to: "", granularity: "day" },
  event: "tip"
};
const statisticsLabels = {
  show: "曝光", click: "点击", page_view: "页面浏览", item_detail_view: "详情浏览", item_open: "打开商品",
  content_start: "开始阅读", content_progress: "阅读进度", content_finish: "完成阅读", content_exit: "退出阅读",
  content_next: "下一项", content_prev: "上一项", like: "点赞", unlike: "取消点赞", favorite: "收藏",
  unfavorite: "取消收藏", share: "分享", comment: "评论", comment_reply: "回复评论", comment_delete: "删除评论",
  comment_report: "举报评论", collection_add: "加入清单", collection_remove: "移出清单", purchase: "购买",
  tip: "打赏", recharge: "充值", search: "搜索", search_result_show: "搜索曝光", search_result_click: "搜索点击",
  ad_show: "广告曝光", ad_click: "广告点击", subscribe: "订阅", unsubscribe: "取消订阅", follow: "关注",
  unfollow: "取消关注", download_start: "开始下载", download_finish: "下载完成", login: "登录", register: "注册"
};
function statisticsDate(offset = 0) {
  return new Intl.DateTimeFormat("en-CA", {timeZone: "Asia/Shanghai", year: "numeric", month: "2-digit", day: "2-digit"}).format(new Date(Date.now() + offset * 86400000));
}
function statisticsReset() {
  statisticsState.request++;
  statisticsState.data = null; statisticsState.apps = []; statisticsState.loadedApps = false;
  statisticsState.loading = false; statisticsState.error = ""; statisticsState.filters.appid = "";
}
async function loadStatistics(force = false) {
  if (!hasSession() || (statisticsState.loading && !force) || (!force && statisticsState.data)) return;
  const request = ++statisticsState.request;
  const session = state.accessToken;
  statisticsState.loading = true; statisticsState.error = ""; statisticsState.data = null;
  setFeedback(""); renderWorkspace();
  try {
    if (!statisticsState.loadedApps) {
      const apps = [];
      for (let page = 1; ; page++) {
        const result = await requestAdminData(`/api/v1/admin/app?page=${page}&page_size=100`);
        apps.push(...(result.items || []));
        if (!result.items?.length || apps.length >= result.total) break;
      }
      if (request !== statisticsState.request || session !== state.accessToken) return;
      statisticsState.apps = apps; statisticsState.loadedApps = true;
    }
    const f = statisticsState.filters;
    if (!f.appid && statisticsState.apps.length) f.appid = String(statisticsState.apps[0].id);
    if (!f.from) { f.from = statisticsDate(-6); f.to = statisticsDate(); }
    if (f.appid) {
      const data = await requestAdminData(`/api/v1/admin/statistics?${new URLSearchParams(f)}`);
      if (request !== statisticsState.request || session !== state.accessToken) return;
      statisticsState.data = data;
    }
  } catch (error) {
    if (request !== statisticsState.request || session !== state.accessToken) return;
    statisticsState.error = error.message || "统计查询失败";
  } finally {
    if (request === statisticsState.request) { statisticsState.loading = false; if (state.activeView === "statistics") renderWorkspace(); }
  }
}
function renderStatisticsFilters() {
  const f = statisticsState.filters;
  filterBar.innerHTML = `<form id="statistics-filter" class="filter-form statistics-filter">
    <label class="form-row"><span class="form-label">应用</span><select name="appid" required aria-label="选择应用">${statisticsState.apps.map(app => `<option value="${escapeHTML(app.id)}" ${String(app.id) === f.appid ? "selected" : ""}>${escapeHTML(app.name)} · ${escapeHTML(app.id)}</option>`).join("")}</select></label>
    <label class="form-row"><span class="form-label">开始日期</span><input name="from" type="date" required value="${escapeHTML(f.from)}" /></label>
    <label class="form-row"><span class="form-label">结束日期</span><input name="to" type="date" required value="${escapeHTML(f.to)}" /></label>
    <label class="form-row"><span class="form-label">统计粒度</span><select name="granularity"><option value="day" ${f.granularity === "day" ? "selected" : ""}>按天</option><option value="hour" ${f.granularity === "hour" ? "selected" : ""}>按小时</option></select></label>
    <button class="button secondary" type="submit" ${statisticsState.loading ? "disabled" : ""}>查询</button>
    <div class="statistics-presets"><button class="button ghost" type="button" data-stat-days="7">最近 7 天</button><button class="button ghost" type="button" data-stat-days="30">最近 30 天</button></div>
  </form>`;
}
function statisticsNumber(n) { return n == null ? "—" : new Intl.NumberFormat("zh-CN", {maximumFractionDigits: 2}).format(n); }
function statisticsPercent(n) { return n == null ? "暂无" : `${(n * 100).toFixed(2)}%`; }
function statisticsTime(value) { return new Intl.DateTimeFormat("zh-CN", {timeZone: "Asia/Shanghai", dateStyle: "short", timeStyle: "short", hour12: false}).format(new Date(value * 1000)); }
function statisticsChart(title, points, series, percent = false) {
  const width = 480, height = 240, left = 64, right = 16, top = 24, bottom = 40;
  const values = series.flatMap(s => points.map(p => s.value(p)).filter(n => n != null));
  const max = Math.max(percent ? 0.01 : 1, ...values);
  const x = i => left + i * (width - left - right) / Math.max(1, points.length - 1);
  const y = n => top + (1 - n / max) * (height - top - bottom);
  const format = percent ? statisticsPercent : statisticsNumber;
  const ticks = [0, .5, 1].map(f => `<line x1="${left}" y1="${y(max*f)}" x2="${width-right}" y2="${y(max*f)}" class="stat-grid"/><text x="${left-8}" y="${y(max*f)+4}" text-anchor="end">${escapeHTML(format(max*f))}</text>`).join("");
  const paths = series.map(s => {
    let started = false;
    const path = points.map((p,i) => { const v = s.value(p); if (v == null) {started = false; return "";} const segment = `${started ? "L" : "M"}${x(i).toFixed(2)},${y(v).toFixed(2)}`; started = true; return segment; }).join(" ");
    const dots = points.map((p,i) => { const v = s.value(p); return v == null ? "" : `<circle cx="${x(i)}" cy="${y(v)}" r="3" fill="${s.color}" tabindex="0"><title>${escapeHTML(p.time.slice(0,16).replace("T"," "))} · ${escapeHTML(s.label)} ${escapeHTML(format(v))}${p.partial ? "（统计中）" : ""}</title></circle>`; }).join("");
    return `<path d="${path}" fill="none" stroke="${s.color}" stroke-width="2"/>${dots}`;
  }).join("");
  const axis = points.length ? `<text x="${left}" y="${height-12}">${escapeHTML(points[0].time.slice(5,16).replace("T"," "))}</text><text x="${width-right}" y="${height-12}" text-anchor="end">${escapeHTML(points.at(-1).time.slice(5,16).replace("T"," "))}</text>` : "";
  return `<section class="statistics-chart"><h4>${escapeHTML(title)}</h4><div class="statistics-legend">${series.map(s => `<span><i style="background:${s.color}"></i>${escapeHTML(s.label)}</span>`).join("")}</div>${values.length ? `<svg viewBox="0 0 ${width} ${height}" role="img" aria-label="${escapeHTML(title)}"><title>${escapeHTML(title)}，精确值见下方数据明细</title>${ticks}${paths}${axis}</svg>` : `<div class="statistics-empty">暂无可计算数据</div>`}</section>`;
}
function renderStatistics() {
  pagination.innerHTML = "";
  if (statisticsState.loading) { panelContent.innerHTML = `<div class="statistics-skeleton" role="status" aria-label="正在加载统计"><div></div><div></div><div></div></div>`; return; }
  if (statisticsState.error) { panelContent.innerHTML = `<div class="statistics-notice error" role="alert">${escapeHTML(statisticsState.error)}，请检查查询范围后重试。</div>`; return; }
  const d = statisticsState.data;
  if (!d) { panelContent.innerHTML = `<div class="statistics-empty">请先创建应用，再查看上报与行为统计。</div>`; return; }
  const ready = d.points.some(p => p.counts != null), summary = d.summary;
  const cards = [
    ["累计商品",d.snapshot?.item_total], ["当前可用商品",d.snapshot?.item_available],
    ["期间上报记录",ready ? summary.item_reports : null], ["首次上报商品",ready ? summary.new_items : null],
    ["商品曝光",ready ? summary.exposures : null], ["商品点击",ready ? summary.clicks : null],
    ["页面浏览",ready ? summary.events.page_view || 0 : null], ["详情浏览",ready ? summary.events.item_detail_view || 0 : null],
    ["打赏",ready ? summary.events.tip || 0 : null], ["购买",ready ? summary.events.purchase || 0 : null]
  ];
  const info = d.snapshot ? `计算于 ${statisticsTime(d.snapshot.generated_at)} · 历史起点 ${escapeHTML(d.snapshot.history_since)} · 北京时间` : "统计任务尚未发布结果";
  const notices = [d.stale ? "统计结果尚未就绪或超过 30 分钟未更新。" : "", d.missing_buckets ? `${d.missing_buckets} 个时间段尚无统计结果，趋势保留空缺。` : "", summary.missing_event_ids ? `${statisticsNumber(summary.missing_event_ids)} 条行为缺少事件 ID，无法保证跨请求去重。` : ""].filter(Boolean);
  const event = statisticsState.event;
  const events = [...new Set([...Object.keys(statisticsLabels),...Object.keys(summary.events)])];
  const count = field => p => p.counts == null ? null : p.counts[field];
  const behavior = key => p => p.counts == null ? null : p.counts.events[key] || 0;
  panelContent.innerHTML = `<div class="statistics-page">
    <div class="statistics-status">${info}</div>
    ${notices.map(n => `<div class="statistics-notice" role="status">${escapeHTML(n)}</div>`).join("")}
    <div class="statistics-cards">${cards.map(([label,value]) => `<div class="statistics-card"><span>${label}</span><strong>${statisticsNumber(value)}</strong></div>`).join("")}<div class="statistics-card"><span>期间 CTR</span><strong>${statisticsPercent(d.ctr)}</strong></div></div>
    <div class="statistics-chart-grid">
    ${statisticsChart("商品上报趋势",d.points,[{label:"上报记录",value:count("item_reports"),color:"#0f6b5c"},{label:"首次上报商品",value:count("new_items"),color:"#b85f3d"}])}
    ${statisticsChart("曝光与点击趋势",d.points,[{label:"商品曝光",value:count("exposures"),color:"#547694"},{label:"商品点击",value:count("clicks"),color:"#0f6b5c"}])}
    ${statisticsChart("点击率 CTR",d.points,[{label:"点击 / 曝光",value:p=>p.ctr,color:"#0f6b5c"}],true)}
    ${statisticsChart("浏览趋势",d.points,[{label:"页面浏览",value:behavior("page_view"),color:"#547694"},{label:"详情浏览",value:behavior("item_detail_view"),color:"#b85f3d"}])}
    ${statisticsChart("打赏与购买趋势",d.points,[{label:"打赏",value:behavior("tip"),color:"#0f6b5c"},{label:"购买",value:behavior("purchase"),color:"#b85f3d"}])}
    <div><label class="statistics-event-label">其他行为 <select id="statistics-event">${events.map(k=>`<option value="${escapeHTML(k)}" ${k===event?"selected":""}>${escapeHTML(statisticsLabels[k]||k)}</option>`).join("")}</select></label>${statisticsChart("行为趋势",d.points,[{label:statisticsLabels[event]||event,value:behavior(event),color:"#547694"}])}</div>
    </div>
    <p class="statistics-help">CTR = 普通商品点击次数 ÷ 普通商品曝光次数。搜索和广告单独统计；曝光为零时无点击率。浏览区分页面与详情，阅读进度单列。当前时间段持续更新，购买表示上报的购买事件次数。已排除 ${statisticsNumber(summary.duplicate_events)} 条重复事件。</p>
    <details class="statistics-details"><summary>查看数据明细与精确数值</summary><div class="table-wrap"><table><thead><tr><th>时间</th><th>上报记录</th><th>去重商品</th><th>点击</th><th>CTR</th><th>页面浏览</th><th>详情浏览</th><th>打赏</th><th>购买</th><th>${escapeHTML(statisticsLabels[event]||event)}</th></tr></thead><tbody>${d.points.map(p=>`<tr><td>${escapeHTML(p.time.slice(0,d.granularity==="day"?10:16).replace("T"," "))}${p.partial?" · 统计中":""}</td><td>${statisticsNumber(p.counts?.item_reports)}</td><td>${statisticsNumber(p.reported_items)}</td><td>${statisticsNumber(p.counts?.clicks)}</td><td>${statisticsPercent(p.ctr)}</td>${["page_view","item_detail_view","tip","purchase",event].map(k=>`<td>${statisticsNumber(p.counts==null?null:p.counts.events[k]||0)}</td>`).join("")}</tr>`).join("")}</tbody></table></div></details>
  </div>`;
}
