import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import test from "node:test";
import vm from "node:vm";

const source = readFileSync(new URL("../internal/http/ui/static/assets/recommendation.js", import.meta.url), "utf8");
function setup() {
  const requests = [], events = {};
  const context = vm.createContext({
    escapeHTML: value => String(value ?? "").replace(/[&<>"']/g, c => ({"&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;"})[c]),
    formatTime: value => value || "", readFormData: form => form.values,
    requestAdminData: async (endpoint, options) => requests.push({endpoint, method: options.method, body: options.json ? JSON.parse(JSON.stringify(options.json)) : undefined}),
    panelContent: {addEventListener: (name, callback) => { events[name] = callback; }},
    state: {activeView: "experiments"}, renderWorkspace: () => {}, structuredClone,
  });
  vm.runInContext(source + `
    loadRecommendation = async () => {};
    globalThis.ui = {state: recommendationState, render: renderRecommendationForm, save: saveRecommendationForm, bind: bindRecommendationEvents};
  `, context);
  const ui = context.ui;
  ui.state.appid = "100001";
  ui.state.sources = [{recall_id: "icf", name: "Item CF", kind: "item"}, {recall_id:"hot_day", name:"日榜", kind:"global", producer:"hot", hot_period:"day"}];
  ui.state.workspace = {strategies: []}; ui.bind();
  return {ui, requests, events};
}
function form(values, selected={}) {
  return {id:"recommendation-form", values,
    querySelectorAll: selector => (selected[selector.match(/name="(.*?)"/)[1]] || []).map(value => ({value})),
    querySelector: () => ({disabled:false})};
}

test("hot recall uses canonical key and create-only endpoint", async () => {
  const {ui, events, requests} = setup();
  ui.state.draft = {kind:"source", value:{producer:"hot", hot_period:"day"}};
  assert.match(ui.render(), /name="key_template"[^>]*value="hot_\{appid\}_day"[^>]*readonly/);
  const fields = {producer:{value:"hot"}, kind:{value:"item"}, hot_period:{value:"newest"}, key_template:{value:""}};
  events.change({target:{name:"producer", form:{id:"recommendation-form", elements:{namedItem:name=>fields[name]}}}});
  assert.equal(fields.key_template.value,"hot_{appid}_newest"); assert.equal(fields.kind.value,"global");
  await ui.save(form({recall_id:"latest", name:"最新", producer:"hot", hot_period:"newest", kind:"global", key_template:fields.key_template.value}));
  assert.equal(requests[0].endpoint,"/api/v1/admin/recommendation/recalls");
  assert.equal(requests[0].body.recall_id,"latest"); assert.equal("version" in requests[0].body,false);
});

test("the five behavior ICF sources choose item keys and submit the selected producer", async () => {
  const {ui, events, requests} = setup();
  ui.state.draft = {kind: "source", value: {}};
  const html = ui.render();
  const fields = {producer:{value:""}, kind:{value:"global"}, hot_period:{value:"day"}, key_template:{value:"hot_{appid}_day",readOnly:true}};
  const formEl = {id:"recommendation-form", elements:{namedItem:name=>fields[name]}};
  for (const [producer, prefix] of [["click_icf","ckicf"], ["like_icf","like_icf"], ["comshare_icf","comshare_icf"], ["subscribe_icf","subscribe_icf"], ["tipby_icf","tipby_icf"]]) {
    assert.ok(html.includes(`<option value="${producer}"`));
    fields.producer.value = producer;
    events.change({target:{name:"producer", form:formEl}});
    assert.equal(fields.kind.value, "item");
    assert.equal(fields.key_template.value, `${prefix}_{appid}_{itemid}`);
    assert.equal(fields.key_template.readOnly, false);
  }
  await ui.save(form({producer:"click_icf", name:"点击 Item CF", recall_id:"click_icf", kind:"item", key_template:"ckicf_{appid}_{itemid}"}));
  assert.deepEqual(requests[0].body, {
    producer:"click_icf", name:"点击 Item CF", recall_id:"click_icf", kind:"item", hot_period:"",
    key_template:"ckicf_{appid}_{itemid}", value_format:"compact_score",
  });
});

test("recall and multiple fallback routes preserve decimals and disabled flags", async () => {
  const {ui, requests} = setup();
  ui.state.sources.push({recall_id:"hot_hour", name:"小时榜", kind:"global"});
  ui.state.draft = {kind:"strategy", value:{recall:[{recall_id:"icf",weight:.005,recall_rate:.375,disabled:true}]}};
  const html=ui.render();
  assert.match(html,/name="weight_recall_icf"[^>]*value="0.005"/);
  assert.match(html,/name="rate_recall_icf"[^>]*value="37.5"/);
  const values={strategy_id:"feed_202611",name:"首页",recommendation_type:"personalized",rank:"recall_weight",weight_recall_icf:".005",rate_recall_icf:"37.5",disabled_recall_icf:"true",weight_fallback_hot_day:"2",rate_fallback_hot_day:"100",disabled_fallback_hot_day:"false",weight_fallback_hot_hour:"1",rate_fallback_hot_hour:"25",disabled_fallback_hot_hour:"false"};
  await ui.save(form(values,{recall:["icf"],fallback:["hot_day","hot_hour"]}));
  assert.deepEqual(requests[0].body.recall,[{recall_id:"icf",weight:.005,recall_rate:.375,disabled:true}]);
  assert.deepEqual(requests[0].body.fallback,[{recall_id:"hot_day",weight:2,recall_rate:1,disabled:false},{recall_id:"hot_hour",weight:1,recall_rate:.25,disabled:false}]);
  assert.equal("fallback_source_id" in requests[0].body,false);
});

test("related strategy always submits empty fallback", async () => {
  const {ui,requests}=setup(); ui.state.draft={kind:"strategy",value:{recommendation_type:"related"}};
  assert.match(ui.render(),/data-rec-personalized[^>]*hidden disabled/);
  await ui.save(form({strategy_id:"related_feed",name:"相关",recommendation_type:"related",rank:"recall_weight",weight_recall_icf:"1",rate_recall_icf:"100"},{recall:["icf"],fallback:["hot_day"]}));
  assert.deepEqual(requests[0].body.fallback,[]);
});

test("experiment has fixed hashing and references immutable IDs only", async () => {
  const {ui,requests}=setup();
  ui.state.workspace.strategies=[{strategy_id:"feed",name:"首页",recommendation_type:"personalized"}];
  ui.state.draft={kind:"experiment",value:{}};
  assert.match(ui.render(),/SHA1/); assert.doesNotMatch(ui.render(),/name="(salt|control_|version_)/);
  await ui.save(form({experiment_id:"trial",name:"实验",bucket_by:"user_id",start_feed:"10",percent_feed:"20"},{strategy:["feed"]}));
  assert.deepEqual(requests[0].body,{experiment_id:"trial",name:"实验",bucket_by:"user_id",assignments:[{strategy_id:"feed",bucket_start:10,bucket_end:29}]});
});

test("a single strategy can receive all one hundred buckets", async () => {
  const {ui,requests}=setup();
  ui.state.workspace.strategies=[{strategy_id:"feed",name:"首页",recommendation_type:"personalized"}];
  ui.state.draft={kind:"experiment",value:{}};
  assert.match(ui.render(),/name="percent_feed"[^>]*max="100"/);
  await ui.save(form({experiment_id:"full_rollout",name:"全量",bucket_by:"user_id",start_feed:"0",percent_feed:"100"},{strategy:["feed"]}));
  assert.deepEqual(requests[0].body.assignments,[{strategy_id:"feed",bucket_start:0,bucket_end:99}]);
});

test("delete and publication submit no revision or version", async () => {
  const {ui,requests,events}=setup();
  const click=async(action,value)=>events.click({target:{closest:()=>({dataset:{recAction:action,recValue:value}})}});
  await click("delete-source","icf");
  assert.equal(requests[0].method,"DELETE"); assert.match(requests[0].endpoint,/recalls\/icf$/);
  ui.state.workspace.experiments=[{experiment_id:"trial"}];
  await click("publish","0"); await click("stop","");
  assert.deepEqual(requests[1].body,{experiment_id:"trial"}); assert.deepEqual(requests[2].body,{experiment_id:""});
});
