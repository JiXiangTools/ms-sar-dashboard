import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import test from "node:test";
import vm from "node:vm";

const source = readFileSync(new URL("../internal/http/ui/static/assets/recommendation.js", import.meta.url), "utf8");

function setup() {
  const requests = [], events = {};
  const context = vm.createContext({
    escapeHTML: value => String(value ?? "").replace(/[&<>"']/g, c => ({"&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;"})[c]),
    formatTime: value => value || "",
    readFormData: form => form.values,
    requestAdminData: async (endpoint, options) => requests.push({endpoint, body: JSON.parse(JSON.stringify(options.json))}),
    panelContent: {addEventListener: (name, callback) => { events[name] = callback; }},
    state: {activeView: "experiments"},
    renderWorkspace: () => {},
    structuredClone,
  });
  vm.runInContext(source + `
    loadRecommendation = async () => {};
    globalThis.ui = {state: recommendationState, render: renderRecommendationForm, save: saveRecommendationForm, bind: bindRecommendationEvents};
  `, context);
  const ui = context.ui;
  ui.state.appid = "app";
  ui.state.sources = [{source_id: "icf", name: "Item CF", kind: "item"}];
  ui.state.workspace = {strategies: []};
  ui.bind();
  return {ui, requests, events};
}

function form(values, checked) {
  return {
    id: "recommendation-form", values,
    querySelectorAll: () => checked.map(value => ({value})),
    querySelector: () => ({disabled: false}),
  };
}

function versionedExperiment(ui) {
  const original = {strategy_id: "alpha", name: "原策略", version: 1, recommendation_type: "personalized"};
  ui.state.workspace.strategies = [{...original, name: "新策略", version: 2, recommendation_type: "related"}];
  ui.state.draft = {kind: "experiment", value: {
    experiment_id: "trial", name: "实验", experiment_version: 3, bucket_by: "user_id", salt: "salt",
    assignments: [{strategy_id: "alpha", strategy_version: 1, bucket_start: 10, bucket_end: 29}],
    snapshot: {strategies: [original]},
  }};
}

test("daily and newest source forms use separate fixed keys", () => {
  const {ui, events} = setup();
  ui.state.draft = {kind: "source", value: {producer: "hot", hot_period: "day", key_template: "hot_{appid}_legacy"}};
  assert.match(ui.render(), /name="key_template"[^>]*value="hot_\{appid\}_day"[^>]*readonly/);
  const fields = {producer: {value: "hot"}, kind: {value: "item"}, hot_period: {value: "day"}, key_template: {value: "icf_{appid}_{itemid}"}};
  const sourceForm = form({}, []);
  sourceForm.elements = {namedItem: name => fields[name]};
  events.change({target: {name: "producer", dataset: {}, form: sourceForm}});
  assert.equal(fields.key_template.value, "hot_{appid}_day");
  assert.equal(fields.key_template.readOnly, true);
  assert.equal(fields.kind.value, "global");
  fields.hot_period.value = "newest";
  events.change({target: {name: "hot_period", dataset: {}, form: sourceForm}});
  assert.equal(fields.key_template.value, "hot_{appid}_newest");
  fields.producer.value = "external";
  events.change({target: {name: "producer", dataset: {}, form: sourceForm}});
  assert.equal(fields.key_template.readOnly, false);
});

test("copying an experiment retains its pinned strategy version and recommendation type", async () => {
  const {ui, requests} = setup();
  versionedExperiment(ui);
  const html = ui.render();
  const personal = html.split("个性化推荐流量</legend>")[1].split("</fieldset>")[0];
  assert.match(personal, /value="alpha" checked/);
  assert.match(personal, /value="1" selected/);
  assert.match(personal, /v2/);
  await ui.save(form({experiment_id: "trial", name: "实验", bucket_by: "user_id", salt: "salt", version_alpha: "1", start_alpha: "10", percent_alpha: "20"}, ["alpha"]));
  assert.deepEqual(requests[0].body.assignments, [{strategy_id: "alpha", strategy_version: 1, bucket_start: 10, bucket_end: 29}]);
});

test("copying an experiment keeps the control version even when the latest strategy changed type", async () => {
  const {ui, requests} = setup();
  versionedExperiment(ui);
  const baseline = {strategy_id: "base", name: "原对照", version: 1, recommendation_type: "personalized"};
  ui.state.draft.value.controls = [{strategy_id: "base", strategy_version: 1}];
  ui.state.draft.value.snapshot.strategies.push(baseline);
  ui.state.workspace.strategies.push({...baseline, version: 2, recommendation_type: "related"});
  const html = ui.render();
  const personal = html.split("个性化推荐流量</legend>")[1].split("</fieldset>")[0];
  assert.match(personal, /name="control_personalized"/);
  assert.match(personal, /value="base:1" selected/);
  await ui.save(form({experiment_id: "trial", name: "实验", bucket_by: "user_id", salt: "salt", version_alpha: "1", start_alpha: "10", percent_alpha: "20", control_personalized: "base:1", control_related: "base:2"}, ["alpha"]));
  assert.deepEqual(requests[0].body.controls, [{strategy_id: "base", strategy_version: 1}]);
});

test("one experiment submits separate fixed controls for both participating recommendation types", async () => {
  const {ui, requests} = setup();
  const strategies = [
    {strategy_id: "alpha", name: "个性化实验", version: 1, recommendation_type: "personalized"},
    {strategy_id: "beta", name: "相关实验", version: 1, recommendation_type: "related"},
    {strategy_id: "base_p", name: "个性化对照", version: 3, recommendation_type: "personalized"},
    {strategy_id: "base_r", name: "相关对照", version: 4, recommendation_type: "related"},
  ];
  ui.state.workspace.strategies = strategies;
  ui.state.draft = {kind: "experiment", value: {}};
  await ui.save(form({experiment_id: "trial", name: "实验", bucket_by: "user_id", version_alpha: "1", start_alpha: "1", percent_alpha: "29", version_beta: "1", start_beta: "1", percent_beta: "29", control_personalized: "base_p:3", control_related: "base_r:4"}, ["alpha", "beta"]));
  assert.deepEqual(requests[0].body.controls, [{strategy_id: "base_p", strategy_version: 3}, {strategy_id: "base_r", strategy_version: 4}]);
});

test("submitting a changed treatment version selects the matching type's control", async () => {
  const {ui, requests} = setup();
  versionedExperiment(ui);
  ui.state.workspace.strategies.push({strategy_id: "base", name: "相关对照", version: 2, recommendation_type: "related"});
  await ui.save(form({experiment_id: "trial", name: "实验", bucket_by: "user_id", version_alpha: "2", start_alpha: "1", percent_alpha: "29", control_related: "base:2"}, ["alpha"]));
  assert.deepEqual(requests[0].body.controls, [{strategy_id: "base", strategy_version: 2}]);
});

test("explicitly changing strategy version moves its flow group and retains the entered experiment", () => {
  const {ui, events} = setup();
  versionedExperiment(ui);
  const editedForm = form({experiment_id: "trial", name: "调整后的实验", bucket_by: "anonymous_id", salt: "new-salt", version_alpha: "2", start_alpha: "40", percent_alpha: "19"}, ["alpha"]);
  events.change({target: {name: "version_alpha", value: "2", dataset: {recStrategyVersion: "alpha"}, form: editedForm}});
  const html = ui.render();
  const related = html.split("相关推荐流量</legend>")[1].split("</fieldset>")[0];
  assert.match(related, /value="alpha" checked/);
  assert.match(related, /value="2" selected/);
  assert.match(related, /name="start_alpha"[^>]*value="40"/);
  assert.match(html, /value="调整后的实验"/);
  assert.match(html, /value="anonymous_id" selected/);
  assert.match(html, /value="new-salt"/);
});

test("editing a strategy preserves disabled recall sources and fractional settings", async () => {
  const {ui, requests} = setup();
  ui.state.draft = {kind: "strategy", value: {
    strategy_id: "alpha", name: "策略", version: 1, recommendation_type: "personalized",
    recall: [{source_id: "icf", weight: 0.005, recall_rate: 0.375, disabled: true}],
  }};
  const html = ui.render();
  assert.match(html, /name="disabled_icf"/);
  assert.match(html, /value="true" selected/);
  assert.match(html, /name="weight_icf"[^>]*value="0.005"/);
  assert.match(html, /name="rate_icf"[^>]*value="37.5"/);
  await ui.save(form({strategy_id: "alpha", name: "策略", recommendation_type: "personalized", rank: "recall_weight", weight_icf: "0.005", rate_icf: "37.5", disabled_icf: "true", newest_percent: "0"}, ["icf"]));
  assert.deepEqual(requests[0].body.recall, [{source_id: "icf", weight: 0.005, recall_rate: 0.375, disabled: true}]);
});

test("related strategy submissions clear supplemental personalized settings", async () => {
  const {ui, requests} = setup();
  ui.state.draft = {kind: "strategy", value: {}};
  await ui.save(form({strategy_id: "related", name: "相关推荐", recommendation_type: "related", rank: "recall_weight", weight_icf: "1", rate_icf: "100", disabled_icf: "false", fallback_source_id: "hot", newest_source_id: "newest", newest_percent: "20", newest_scenes: "home"}, ["icf"]));
  assert.equal(requests[0].body.fallback_source_id, "");
  assert.equal("newest" in requests[0].body, false);
});

test("both strategy types offer newest in recall and show the requested notice", async () => {
  for (const kind of ["personalized", "related"]) {
    const {ui, requests} = setup();
    ui.state.sources.push({source_id: "latest", name: "最新上架", kind: "global", producer: "hot", hot_period: "newest"});
    ui.state.draft = {kind: "strategy", value: {recommendation_type: kind}};
    const html = ui.render();
    assert.match(html, /如不添加最新的召回， 新品可能无法推荐！/);
    assert.match(html, /name="source" value="latest"/);
    assert.doesNotMatch(html, /name="newest_(source_id|percent|scenes)"/);
    await ui.save(form({strategy_id: "alpha", name: "策略", recommendation_type: kind, rank: "recall_weight", weight_latest: "5", rate_latest: "25", disabled_latest: "false"}, ["latest"]));
    assert.deepEqual(requests[0].body.recall, [{source_id: "latest", weight: 5, recall_rate: .25, disabled: false}]);
    assert.equal("newest" in requests[0].body, false);
  }
});

test("editing a legacy newest stage converts it to a recall without mutating history", () => {
  const {ui} = setup();
  ui.state.sources.push({source_id: "latest", name: "最新上架", kind: "global", producer: "hot", hot_period: "newest"});
  const original = {recommendation_type: "personalized", recall: [{source_id: "icf", weight: 3, recall_rate: 1}], newest: {source_id: "latest", share_percent: 25, scenes: ["discover"]}};
  ui.state.draft = {kind: "strategy", value: original};
  const html = ui.render();
  assert.match(html, /name="source" value="latest" checked/);
  assert.match(html, /name="weight_latest"[^>]*value="1"/);
  assert.match(html, /name="rate_latest"[^>]*value="25"/);
  assert.equal(original.recall.length, 1);
  assert.equal(original.newest.share_percent, 25);
});

test("newest notice follows enabled recall selection for either strategy type", () => {
  for (const kind of ["personalized", "related"]) {
    const {ui, events} = setup();
    ui.state.sources.push({source_id: "latest", name: "最新上架", kind: "global", producer: "hot", hot_period: "newest"});
    ui.state.draft = {kind: "strategy", value: {recommendation_type: kind}};
    const notice = {hidden: false};
    const editedForm = form({disabled_latest: "false"}, ["latest"]);
    editedForm.querySelector = () => notice;
    const target = {name: "source", dataset: {}, form: editedForm};
    events.change({target});
    assert.equal(notice.hidden, true);
    editedForm.values.disabled_latest = "true";
    target.name = "disabled_latest";
    events.change({target});
    assert.equal(notice.hidden, false);
    editedForm.values.disabled_latest = "false";
    editedForm.querySelectorAll = () => [];
    target.name = "source";
    events.change({target});
    assert.equal(notice.hidden, false);
  }
});
