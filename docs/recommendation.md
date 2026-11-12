# 推荐策略与实验版本

逐商品推荐契约见 [Offline 推荐实验](../../alg-rec-offline/docs/recommendation-experiments.md)。统一 items 替换 item_ids/ad_ids/extra；Toonva 卡片按自身 ID 保留 rec_id、from、source_id、ad、experiment_id、strategy_id，曝光/点击沿用原卡片归因。无实验字段为 null，广告不带实验分组。

## 配置与发布

后台「推荐实验」按全局召回源、应用策略、实验版本三个层次管理。一个 App 可以保存多个策略和实验草稿，同一时刻只运行一个实验。策略可被不同实验复用。

1. 新建全局召回源：名称、source_id、读取类型、数据产出方和 Key 模板。模板使用 `{appid}`、`{itemid}`、`{userid}`，不写 `*` 通配符。Item CF 示例 `icf_{appid}_{itemid}`；外部 User CF 示例 `ucf_{appid}_{userid}`。
2. 新建应用策略：选择 `recommendation_type=personalized`（个性化推荐）或 `related`（相关推荐），填写中文名称、strategy_id、召回源及各自的权重和召回比例。两类策略均可把最新上架添加到 recall，参与统一过滤、去重和权重排序，不单独预留新品位置；个性化推荐另可配置全局兜底榜单，相关推荐只执行召回与排序，禁止配置 fallback，候选不足时返回实际数量。未选择并启用最新召回时，后台提示「如不添加最新的召回， 新品可能无法推荐！」。当前排序只支持 `recall_weight`。热门接口不参与策略与实验分配。
3. 创建实验版本：选择已有策略版本、身份字段和整数流量范围，统一使用稳定哈希分桶。每个参与实验的推荐类型还必须选择一个固定版本的默认对照策略，使用与实验分组不同的策略 ID。后台复制实验保留原策略和对照版本，升级需明确选择；切换版本后按该版本的推荐类型显示流量配置。保存时冻结所有所选策略及引用的召回源版本。
4. 发布草稿：原运行版本结束，新版本开始。编辑召回源或策略只生成新定义版本；调整运行效果必须创建并发布新的实验版本，不能原地改变旧快照。
5. 停止实验：发布 `experiment:null`，Online 使用本地默认配置。再次使用旧方案时，也创建新实验版本；已结束版本不能重新开启。

`experiment_id` 是稳定的实验标识，`experiment_version` 从 1 递增；配置发布序号 `revision` 按 App 递增，停止实验也占一个序号。创建草稿使用当前最新版本作为乐观锁，发布使用 `expected_revision`，过期编辑返回 409。

## 分桶规则

- strategy_id 匹配 `[a-z][a-z_]{0,9}`，`default` 为保留对照组。source_id / experiment_id 匹配 `[a-z][a-z0-9_]{0,47}`。
- `bucket_by` 为 `user_id` 或 `anonymous_id`。Online 请求应传递所选身份，身份最长 128 字符。
- 数字和字符串身份（例如 web_k123）统一按原字符串使用 SHA-256，桶为 0–99。每策略 1–99 个桶，例如范围 1–29 占 29%。同一实验内两类推荐共用稳定桶，按类型分别分配流量，同类型范围不能重叠，不同类型可使用相同范围。只有关联了该类型策略的接口参与实验，其剩余流量为 default；未配置类型不参与实验，返回 experiment_id/strategy_id=null。两类分组使用同一身份哈希，不能视为相互独立的随机实验。001 与 1 是不同字符串身份。
- 输入为 `appid + NUL + experiment_id + NUL + salt + NUL + identity`，摘要前 8 字节按大端无符号整数 `% 100`。版本号不参与哈希，保持相同身份稳定；修改 salt、范围或身份字段需新实验版本。bucket_algorithm 固定 sha256，创建 API 可以省略，服务端填充。
- 缺少所选身份的请求不进入实验，返回 experiment_id/strategy_id=null，决策原因记为 missing_bucket_identity。

## 历史与生效区间

PostgreSQL 的 `t_rec_source_version`、`t_rec_strategy_version` 和 `t_rec_experiment_version` 保存不可变配置历史。实验状态为 draft / running / ended，分别记录 created_at、started_at、ended_at。草稿尚未生效，后两项为空；运行中 ended_at 为空。

策略定义记录创建时间。策略每次被实验版本使用都有独立的生效/结束区间，工作区的 `strategy_history` 返回该使用历史；对照策略的 `group_id=default`，页面标明默认对照。复用策略不会改写过去的结束时间。配置发布第一次成功写 Redis 的时间作为生效边界；DB 确认重试沿用同一时间。实际请求以 rec_id 决策中的版本为准，避免只按时钟边界推断分组。

## 共享 Redis 与一致性

Dashboard、Online、Offline 连接同一个 Redis，生产为 Cluster DB 0。Dashboard Compose 不再启动独立 Redis。三端的配置前缀必须一致：`MSSAR_REDIS_KEY_PREFIX`、`MSRO_REDIS_KEY_PREFIX`、`AROF_REDIS_KEY_PREFIX`，默认为空。

每个 App 只有一个最新完整配置：`<prefix>rec:appconf:{appid}`，普通 SET，无 TTL。历史仅保存在数据库。Online 每次推荐读取此配置，Offline 每次任务读取；均不请求 Dashboard 配置接口，不获取刷新锁，也没有 10 分钟回源流程。Redis 配置、模板和决策队列的完整数据契约由 [Offline 实验文档](../../alg-rec-offline/docs/recommendation-experiments.md) 维护。

`t_rec_publication` 是发布 outbox：先提交待发布快照，再用单 Key Lua 按 revision 替换 Redis，最后确认数据库生命周期。同 revision 重试幂等，旧 revision 不能覆盖新配置。发布失败返回 503 并保留待发布状态，每 5 秒重试；存在待发布项时不允许用另一次发布覆盖它。Dashboard 启动时及运行中每分钟重放各 App 最新配置，修复缺失、损坏 JSON、错误 Redis 类型和同 revision 的内容漂移；首次恢复失败会继续重试完整恢复。恢复已确认配置保留原发布时间与实验生效区间。这是 Dashboard 的投影恢复，Online 不承担配置回源。

Online 在配置不存在、无启用实验或配置不可解析时使用已有 `configs/recommand-settings.json` 默认策略；失效原因进入决策记录。新实验的 default 流量使用快照中固定的对照策略，修改本地默认配置不会改变实验基线。对照组仍标记 strategy_id=default，决策额外记录 effective_strategy_id 和 strategy_version；调整对照配置必须发布新实验版本。历史 schema_version=1 且未固定对照的实验维持旧语义。

## 管理 API

全部使用现有管理员 JWT、统一响应 envelope 和审计日志。

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| GET | `/api/v1/admin/recommendation/sources` | 全局召回源最新定义 |
| POST | `/api/v1/admin/recommendation/sources` | 创建召回源新版本 |
| GET | `/api/v1/admin/recommendation/apps/:appid` | 应用策略、最近 500 个实验版本、发布状态与策略使用历史 |
| POST | `/api/v1/admin/recommendation/apps/:appid/strategies` | 创建策略新版本 |
| POST | `/api/v1/admin/recommendation/apps/:appid/experiments` | 创建实验草稿新版本 |
| POST | `/api/v1/admin/recommendation/apps/:appid/publication` | 发布草稿或停止实验 |

创建召回源（首次 version=0，更新时传最新 version）：

```json
{"source_id":"icf","version":0,"name":"Item CF","kind":"item","producer":"icf","key_template":"icf_{appid}_{itemid}","value_format":"compact_score"}
```

读取类型为 item / user / global；产出方为 icf / ir / nl / hot / external。内置 icf/ir/nl 仅支持 item，hot 仅支持 global，并要求 `hot_period` 为 hour/day/week/month/quarter/all/newest。user 由外部服务产出。权重 >0 且 <=1000，recall_rate >0 且 <=1，每策略 1–16 个召回源。后台支持小数权重、召回比例及单独停用召回源，编辑时保留这些设置。多个来源即使读取同一 Redis key，也分别应用自身权重、配额和启用状态。fallback 必须引用 global 源。最新召回使用 kind=global、producer=hot、hot_period=newest 的来源，例如 hot_{appid}_newest；recall_rate 将该源过滤后的入池候选限制为 ceil(size × recall_rate)，不保证最终结果占比。全局模板的新增不自动新增算法实现。

创建策略：

```json
{"strategy_id":"a","version":0,"recommendation_type":"personalized","name":"策略 A","recall":[{"source_id":"icf","weight":3,"recall_rate":1,"disabled":false},{"source_id":"newest","weight":1,"recall_rate":0.2,"disabled":false}],"fallback_source_id":"","rank":"recall_weight"}
```

相关推荐策略将 `recommendation_type` 设为 `related`，fallback_source_id 留空，同样可关联 newest 召回源。新策略不再接受独立 newest 配置，空值不会写入配置；历史策略和快照仍兼容读取，保持原执行语义。后台编辑旧版本时，将启用的 newest 来源并入 recall（默认权重 1，原 share_percent 转为 recall_rate），已有同名召回保留现有设置，界面明确提示核对配置和场景行为变化；保存新策略并发布新实验版本后生效。新建时缺省类型兼容为 personalized；历史快照中缺少类型也按 personalized 解释，不重写历史。类型变更创建新策略版本，并须在新实验版本中关联才能生效；分桶校验以所关联版本的类型为准。

创建实验（首次 experiment_version=0，strategy_version=0 表示保存时选最新策略版本，也可以固定指定版本）：

```json
{"experiment_id":"exp_home","experiment_version":0,"name":"首页实验","bucket_by":"user_id","bucket_algorithm":"sha256","salt":"","assignments":[{"strategy_id":"a","strategy_version":0,"bucket_start":1,"bucket_end":29}],"controls":[{"strategy_id":"base","strategy_version":1}]}
```

发布：

```json
{"experiment_id":"exp_home","experiment_version":1,"expected_revision":0}
```

停止：

```json
{"experiment_id":"","experiment_version":0,"expected_revision":1}
```

未知字段或非法配置返回 400；不存在的 App/草稿返回 404；版本冲突返回 409；待重试的发布返回 503。所有变更写入 `cate=RECOMMENDATION` 的审计记录。

## 推荐响应与埋点

Online 返回 rec_id 与统一的逐商品 items：

```json
{"rec_id":"r_0123456789abcdef0123456789abcdef","items":[{"item_id":"C10001","from":"nl","source_id":"nl_feed","ad":false,"experiment_id":"exp_home","strategy_id":"a"}],"size":1}
```

个性化和相关推荐接口只执行各自类型的策略。无实验或实验未关联该类型策略时 experiment_id/strategy_id=null，后者决策原因为 recommendation_type_not_in_experiment；参与实验的类型内剩余流量返回相同 experiment_id 和 strategy_id=default。个性化推荐召回不足而执行 fallback 时，仍保留原分组。相关推荐默认配置同样只执行召回与排序，可使用最新召回，不使用兜底或独立上新流程。热门接口不读取实验配置，始终 experiment_id/strategy_id=null，保留 rec_id。实际策略、配置序号、实验/策略版本、推荐类型、兜底/上新数量及返回物品写入 rec_id 对应的决策。newest_count 仅计入最终返回且去重后来源胜出的是最新召回的物品；新品候选因低权重未返回时不计数。

曝光、点击等物品行为在现有平铺 properties 中携带原项的 rec_id、from、source_id、ad，并在实验字段非 null 时携带 experiment_id、strategy_id。客户端不需要传 experiment_version；统计通过决策取原版本，因此版本切换后上报的旧曝光/点击仍归入旧版本。完整示例见 [Receiver API](../../ms-data-receiver/docs/api.md)，统计口径见 [statistics.md](statistics.md)。

## 上线与验证

先运行 `ms-sar-dbtool` 应用迁移 000003，再部署 Dashboard、Offline 的配置读取与 `rec-decision-consumer`、Online 和 Receiver。所有服务使用共享 Cluster DB 0 和相同配置前缀。决策 consumer 与 rec-statistics 共享持久 data 卷；先启动归档，再开放新 Online 流量。接入端把响应元数据随对应物品埋点回传后，实验事件统计才完整。

本次固定对照扩展使用配置 schema_version=2，无新增数据库迁移。先升级支持配置版本 1/2 的 Online 与 Offline，再部署 Dashboard 并发布新实验；旧 Online 使用严格配置解析，不能读取 controls。旧实验不会自动补对照，编辑时需选择并保存新实验版本。已有自定义热门 key 或 appid 拼接未分段的模板应先在旧服务上改为规范模板并发布新实验版本，再升级读取方；历史数据库快照保留不变，非法活动配置会被拒绝，Online 退回本地默认。

模板中的 appid 必须独立为下划线分隔的一段。热门源只接受 hot_{appid}_{hot_period}，不支持自定义热门别名：日榜只能指向 hot_{appid}_day，上新源只能指向 hot_{appid}_newest，由 nearline 维护；external 不得占用 hot_ 命名空间。日榜不处理 newest 的配置，日榜重算和清理保留上新榜的内容及 TTL。原有分类榜沿用独立的分类命名规则。

`go test ./...` 覆盖配置校验、统计合并和原有 API；`node --test test/recommendation_ui_test.mjs` 验证后台版本选择及策略编辑，均纳入 CI。设置独立 `MSSAR_RECOMMENDATION_TEST_DSN` 与 `MSSAR_RECOMMENDATION_TEST_REDIS=host1:port,host2:port,host3:port` 后运行 `go test -tags integration -race ./internal/service`，验证快照、无 TTL 发布、历史区间、并发编辑与失败重试。
