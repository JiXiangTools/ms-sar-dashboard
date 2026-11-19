# 推荐配置与实验

本次采用不可变定义：召回源、策略和实验只有新增、查看、删除；调整任何定义必须使用新的 ID，删除后 ID 仍不可复用。不再使用业务 version、发布 revision、controls、salt 或 bucket_algorithm。`schema_version=3` 只识别数据格式，不参与运营配置或实验分组。不兼容旧配置、旧推荐响应及旧归因决策。

## 定义与应用范围

- 召回源是全局目录，使用 `recall_id` 标识如何读取算法结果。`kind` 支持 item/user/global，`producer` 支持 icf/ir/nl/hot/external 及五路行为 ICF。`hot_period` 决定 hot 的榜单周期和 newest 来源。相同 Redis key 可配置不同 `recall_id`、配额和权重。
- 策略、实验和当前发布配置按 App 隔离。策略以 `(appid,strategy_id)` 唯一，实验以 `(appid,experiment_id)` 唯一；不同 App 可有相同策略 ID。所有定义 ID 匹配 `[a-z][a-z0-9_]{0,47}`。
- 每个 App 可保存多个策略和实验，同一时刻运行一个实验。策略可被多个实验引用；实验创建时保存完整召回与策略快照。
- `default` 是 Online 本地默认策略的保留 ID。Dashboard 不能创建这个 ID。当前使用哪份实验可以变化，发布不做序号比较或 CAS。

`kind` 控制 key 的填充方式：item 使用点击种子/当前物品，user 使用用户身份，global 只填 appid。模板支持 `{appid}`、`{itemid}`、`{userid}`；appid 必须独立为下划线分隔的一段。内置 icf/ir/nl 仅支持 item，hot 仅支持 global。hot_period 为 hour/day/week/month/quarter/all/newest，模板必须是 `hot_{appid}_{hot_period}`；external 不得占用 hot_ 命名空间。新增模板不自动实现新的算法。

## 策略与本地 default

Dashboard 与 `ms-rec-online/configs/recommand-settings.json` 使用相同的召回定义、策略结构、校验、编译和执行逻辑。Online JSON 顶层仅有 `default`，其中含 schema_version、recalls 目录和 personalized/related 各一份 strategy_id=default 的策略。没有 100001 或其他 App 的策略覆盖；每个 App 都使用这套本地默认配置。商品 ID 准入是应用属性，100001 的 positive_int64 约束独立保留在系统 YAML 的 `app.item_id_formats` 中。

策略示例（创建 API 的 appid 取路径）：

```json
{
  "strategy_id":"home_a", "name":"首页策略", "recommendation_type":"personalized",
  "recall":[{"recall_id":"nl_feed","weight":3,"recall_rate":1,"disabled":false},{"recall_id":"hot_newest","weight":1,"recall_rate":0.1,"disabled":false}],
  "fallback":[{"recall_id":"hot_day","weight":2,"recall_rate":1,"disabled":false},{"recall_id":"hot_hour","weight":1,"recall_rate":0.5,"disabled":false}],
  "rank":"recall_weight"
}
```

recall 和 fallback 都是路由数组，元素格式相同；disabled 是布尔值。召回 1–16 路，兜底 0–16 路；weight >0 且 <=1000，recall_rate >0 且 <=1。fallback 只引用 global 来源；related 必须明确提交 `fallback:[]`，只执行召回和排序，候选不足返回实际数量。排序当前只有 recall_weight，由 Online 计算。

每路主召回先按 Offline 列表顺序过滤有效商品并去重，再选取至多 ceil(size × recall_rate) 个候选入池；多点击种子按最近优先顺序共享配额。入池后合并并按权重/分数排序。个性化主结果不足时，以缺口 M 为基数，从每路 fallback 选择至多 ceil(M × recall_rate) 个候选，合并排序补齐 M，不挤掉主召回结果。最新上架是普通全局召回，不再有独立 newest 阶段，也不保证最终结果占比。

商品顶层可选 parent_id 由生成端用于优先排列不同合集，同合集其余商品保留在后部，空值彼此独立。默认输出、实验与 default 采用相同消费规则；newest 保留原始时间候选后派生顺序，搜索按用户输入意图检索。详见 [召回列表消费顺序](../../alg-rec-offline/docs/design.md#51-召回列表的在线消费顺序) 和 [合集重排](../../alg-rec-offline/docs/design.md#52-推荐列表的合集重排)。

## 实验与分桶

```json
{"experiment_id":"exp_home","name":"首页实验","bucket_by":"user_id","assignments":[{"strategy_id":"home_a","bucket_start":1,"bucket_end":29}]}
```

bucket_by 只选 user_id/anonymous_id。固定计算 `uint64_be(SHA1(UTF8(identity))[:8]) % 100`，桶为 0–99；App、实验 ID 不参与哈希，没有算法或盐配置。001 与 1 是不同字符串身份。每策略范围包含 1–100 个桶，支持单条策略覆盖全部流量；同类型范围不可重叠，不同类型可使用相同范围；两类实验共享身份分桶，不能视为独立随机实验。

| 请求情况 | 执行策略 | 返回的实验/策略 ID |
| --- | --- | --- |
| 该类型参与实验、身份有效且命中范围 | 引用的不可变策略 | experiment_id / 实际 strategy_id |
| 该类型参与实验、身份有效但未命中范围 | Online 当前本地 default | experiment_id / default |
| 配置缺失、无实验、配置无效、类型未参与或缺少所选身份 | Online 当前本地 default | null / null |
| hot 接口 | 请求选择的榜单 | null / null |

本地 default 调整会立即影响该进程的默认流量；不固定额外对照定义，不生成默认内容 ID 或摘要 ID。执行 fallback 保留原分组。缺身份原因记为 missing_bucket_identity，未参与类型记为 recommendation_type_not_in_experiment。

## 行为 ICF 首轮实验

在「全局召回源」中可选择 `click_icf`、`like_icf`、`comshare_icf`、`subscribe_icf` 和 `tipby_icf`。表单自动填入物品模板，`click_icf` 使用 `ckicf_{appid}_{itemid}`。Offline 为每路独立计算候选。 [各路事件口径](../../alg-rec-offline/docs/design.md#75-输出到-redis)

首轮配置包含全局召回定义、两个个性化策略和实验草稿。对照组复制 Online 仓库的默认个性化策略。实验组将混合 `icf` 设为权重 `3`、配额 `60%`，启用点击和喜欢两路，各设权重 `2.5`、配额 `10%`。草稿中其余三路停用，每组各分配 `10%` 的登录用户桶。使用自定义 Online 默认策略的环境可按当地配置调整两组的其余路由。 [首轮实验配置](examples/behavior-icf-experiment.json)

1. 用 `.recalls[]` 中的定义创建全局召回源。目录已有同 ID 时可直接引用匹配的定义。
2. 把 `.strategies[]` 中的两份定义逐个提交给目标 App 的策略创建接口，再把 `.experiment` 提交给同一 App 的实验创建接口。
3. 更新 Dashboard、Online 和 Offline 后，可在发布前运行 `rec-icf`，查看 `redis_keys_by_producer` 中点击、喜欢两路的写入数。发布后用看板对比两组整体行为。按 `recall_id` 统计实际曝光时，以 App、`rec_id` 和 `item_id` 关联 `SHOW` 行为与归档决策。

## 发布、历史和删除

共享 Redis 每 App 一个完整快照 `<prefix>rec:appconf:{appid}`，普通 SET，无 TTL。Dashboard、Online、Offline 的 key prefix 必须一致；生产共用 Cluster DB 0。Online 每请求读取，Offline 每任务/派生写入读取，不通过 HTTP 回源。完整 wire 示例和决策归档见 [Offline 实验契约](../../alg-rec-offline/docs/recommendation-experiments.md)。

PostgreSQL 保存 t_rec_recall、t_rec_strategy、t_rec_experiment；删除写 deleted_at 墓碑并保留主键。被未删除策略引用的召回、被未删除实验引用的策略、正在运行或仍是当前发布目标的实验不可删除。先停止实验，再按实验→策略→召回顺序解除引用；历史快照和审计不改写。

实验状态 draft/running/ended，保存 created_at、started_at、ended_at。发布只接受 draft，结束后如需再次使用方案，创建新实验 ID。工作区 strategy_history 按实验保留策略使用区间。实际行为归因通过 rec_id 关联当时决策，不按时间边界猜测。

t_rec_publication 保存最新目标快照和 pending。发布先提交待发布目标，再写 Redis 并确认生命周期；失败返回 503，后台每 5 秒重试。后续发布可以替换待发布目标，不比较发布序号；不处理极端情况下旧 Redis 写入晚到的排序问题。Dashboard 启动和每分钟重放最新配置，修复缺失、错误类型和内容漂移；已确认发布保留 published_at 与生效区间。发布请求必须显式提供字符串 experiment_id；缺失、null 或非字符串返回 400。停止提交 experiment_id 空字符串，下发 `experiment:null,recalls:[],strategies:[]`。

## 管理 API

使用现有管理员 JWT、统一响应和 RECOMMENDATION 审计。

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| GET / POST | `/api/v1/admin/recommendation/recalls` | 查看 / 新增全局召回定义 |
| DELETE | `/api/v1/admin/recommendation/recalls/:id` | 删除未引用召回，保留 ID |
| GET | `/api/v1/admin/recommendation/apps/:appid` | 策略、实验、快照、发布状态和使用历史 |
| POST | `/api/v1/admin/recommendation/apps/:appid/strategies` | 新增策略 |
| DELETE | `/api/v1/admin/recommendation/apps/:appid/strategies/:id` | 删除未引用策略 |
| POST | `/api/v1/admin/recommendation/apps/:appid/experiments` | 新增实验草稿 |
| DELETE | `/api/v1/admin/recommendation/apps/:appid/experiments/:id` | 删除非活动实验 |
| POST | `/api/v1/admin/recommendation/apps/:appid/publication` | `{"experiment_id":"exp_home"}` 发布，空字符串停止 |

召回创建示例：`{"recall_id":"nl_feed","name":"Nearline","kind":"item","producer":"nl","key_template":"nl_{appid}_{itemid}","value_format":"compact_score"}`。没有编辑接口。未知字段/非法配置为 400，不存在为 404，重复 ID/引用阻止删除/非草稿发布为 409，发布待重试为 503。

## 推荐响应和埋点

```json
{"rec_id":"r_0123456789abcdef0123456789abcdef","items":[{"item_id":"C10001","from":"nl","recall_id":"nl_feed","ad":false,"experiment_id":"exp_home","strategy_id":"home_a"}],"size":1}
```

每项保留胜出来源；广告 from=ad、recall_id=ad，实验字段 null。客户端按卡片保存 rec_id/from/recall_id/ad 和实验字段，在平铺 properties 中回传，省略 null 实验字段。旧卡片在新实验发布后上报仍关联原决策和原 ID。决策 schema_version=3；实验统计按 experiment_id+strategy_id+recommendation_type 分组，见 [statistics.md](statistics.md)。

## 本次修改范围与测试环境切换

本次协调变更涉及 Dashboard 定义表/API/页面/发布/统计，Online 默认配置/分桶/执行/逐项响应，Offline 配置读取/决策归档/实验统计，Receiver 归因校验，以及 Toonva 的后端契约与前端卡片埋点。Search 仅同步告警参数名，搜索索引与查询格式不变；广告发布版本和商品/Kafka 来源水位属于其他协议，继续保留。Gagpop 接入同样只使用 items/recall_id 和新 ID 规则。没有旧格式双读或数据迁移适配器。

此项目尚无正式生产数据，000003 改为本次空库基线；schema.sql 的对应校验和已同步。已有测试库不能直接对旧 000003 执行新版启动，应在停写后完成以下切换。ca1 的实际执行范围和结果见下方发布记录。

1. 停止目标测试环境推荐请求、Dashboard 发布重试、决策 consumer、统计及相关行为消费者；处理或清空旧格式测试消息，避免重启后回放。
2. 在专用测试 Dashboard 库重建推荐定义表并移除旧 000003 的迁移记录，再运行新版 ms-sar-dbtool。保留 000001/000002 记录及授权、App 数据。SQL 如下：

```sql
BEGIN;
DROP TABLE IF EXISTS t_rec_publication;
DROP TABLE IF EXISTS t_rec_experiment_version, t_rec_strategy_version, t_rec_source_version;
DROP TABLE IF EXISTS t_rec_experiment, t_rec_strategy, t_rec_recall;
DELETE FROM schema_migrations WHERE version = 3;
COMMIT;
```

3. 清除目标测试 App 在 t_app_statistics_day、t_app_statistics_snapshot 的旧实验聚合；保留其他 App 的记录。统计 revision 仍是聚合上传幂等序号，与已移除的配置 revision 无关。
4. 删除目标前缀的 `rec:appconf:{appid}`；协调该前缀全部 App 切换后删除旧 `rec:decisions` Stream（含旧 consumer group）。按实际模板清除已废弃召回别名；默认算法结果可由新版任务重算。Cluster 查找键时遍历全部主节点。
5. 停止归档后清理测试 data/recommendation-decisions/decisions.sqlite3 及其 -wal/-shm；如卷共用多个 App，按 appid 删除目标测试决策及相关 quarantine，保留其他 App。移除目标 App 的旧归因行为 raw `data/behavior/<appid>/`，同步清理对应行为去重索引/旧 Kafka 消息或使用新的测试事件 ID；保留商品 raw。移除该 App 的 `state/statistics/<appid>-manifest.json` 后重算，保留 statistics/revision.json。
6. 清理业务端旧归因缓存：Toonva Redis `read:rank:*` 榜单缓存、浏览器已有推荐卡片/未发送埋点；重新拉取卡片。按缓存实际共享范围定向清理。
7. 同批部署读取端和生产者，先启动新版归档，再开放 Online 和行为上报。重新创建召回/策略/实验，发布后检查 schema_version=3、无业务版本字段、分桶及 default、fallback 多路归因；运行 Offline 重算并检查实验聚合。

切换记录须保存目标环境、App/前缀、清理范围、执行人和时间；若回退，恢复整套旧程序及对应测试备份，避免混用格式。

验证：`go test ./...`、`node --test test/recommendation_ui_test.mjs`；独立 PostgreSQL + Redis Cluster 使用 `MSSAR_RECOMMENDATION_TEST_DSN`、`MSSAR_RECOMMENDATION_TEST_REDIS` 运行 `go test -tags integration -race ./internal/service`。

2026-11-15 验证记录：Dashboard、Online、Receiver、Search 的全量 Go 测试及 vet 通过；Dashboard 独立 PostgreSQL + Redis Cluster 集成通过；Offline 全量 360 项通过、29 项按依赖条件跳过，临时 Redis 单点/Cluster 验证 48 项通过。Dashboard 表单单测及浏览器多路兜底/相关推荐空兜底提交通过，Toonva API 契约和游客/登录用户归因浏览器测试通过。Toonva 前端全量单测在既有 `build/seo-collection.json` 的 2026-11-12 快照过期处失败，未修改该独立 SEO 数据。

### 实现复核（2026-11-15）

保留不可变定义 ID、按 App 发布、本地统一 default、多路 fallback、身份固定分桶的方案。本次整改范围为 Dashboard、Online、Offline，不增加业务版本、发布序号或旧格式兼容。

- Dashboard 发布入口区分漏传/null 与显式空字符串：前者返回 400，后者才停止实验，避免不完整请求意外停用当前实验。修改 `internal/http/handler/recommendation.go`。
- 取消单条策略最多 99 个桶的限制；单条策略可以覆盖全部 0–99 桶，未分配流量继续执行 default。同步 Dashboard/Online 的领域校验、Dashboard 表单与 Offline 读取校验；Online 回归覆盖全部 100 个桶。
- Offline 读取完整快照时补齐 ID、名称、产出方与读取类型、模板与周期、创建时间校验，错误类型统一按非法配置处理；非法快照在结果写入及重算清理之前失败。修改 `alg_rec_offline/service/recommendation_config.py`。
- Offline 决策归档拒绝空值/非字符串实验 ID、无实验却使用具体策略 ID、热榜携带实验归因的记录，持久隔离后继续处理有效决策，避免污染实验统计。修改 `alg_rec_offline/service/recommendation_decisions.py`。

复核验证：Dashboard/Online 全量 `go test ./...` 与 `go vet ./...` 通过，相关领域/入口/推荐包 race 检查通过；Dashboard 表单 6 项通过；Offline 全量 398 项通过、29 项按依赖条件跳过，隔离认证 Redis 单点及 Cluster 验证 108 项通过。以上为工作区复核阶段结果，当时尚未部署；最终发布提交及切换验收如下。

### ca1 发布记录（2026-11-15）

北京时间 21:43 完成推荐不可变 ID 协调切换。固定源码、镜像及成功 CI 见 [部署记录](../../services-deploy/search-rec-ad/README.md#2026-11-15-ca1-推荐不可变-id已部署)。本批采用本任务独立提交，不包含并行开发的合集 parent_id 功能及其他 Toonva 页面修改。

- 停写并排空旧行为队列后保存完整 SAR PostgreSQL 备份、配置、决策/去重 SQLite 和 App 100001 的行为文件；在隔离恢复库完成旧备份恢复及新版迁移演练后才操作实际测试库。
- 重建空的推荐定义/发布表，重做 000003，保留 000001/000002。App 100001 清除旧实验汇总、572 条决策、630 条行为去重记录、旧行为文件及统计 manifest；清除其 appconf 与已排空的旧决策 Stream。Toonva 定向清除 2 个旧榜单缓存。商品 raw、Kafka 消费组/offset、statistics/revision.json 保留。
- 两个 App 和管理员记录校验和前后一致；App 100002 的 8 天汇总保留。新版归档先于 Online 启动，18 个目标容器正常运行、重启 0、无 OOM，其余容器身份与启动时间未变。
- 热门、个性化、相关各返回 5 条真实推荐，逐项 recall_id/实验字段符合新契约；Toonva 发现流与榜单转发正常，曝光/点击成功上报，决策 schema_version=3 持久归档，队列 pending/lag 为 0、quarantine 为 0。当前没有活动实验，实际流量使用 Online 本地 default。
- Offline 单次重算成功回传 App 100001 的 14 天统计，保留统计上传 revision，商品总数 147、可用 32。HTTPS 网关的版本、发现流和前端页面通过现有证书校验；实验发布与故障链路由固定五仓提交的真实组件 CI E2E 验证，本次未向实际测试库创建验证实验。
- 最终发布源码的 Go 全量测试/vet、Dashboard UI、Toonva 契约/游客与登录归因回归通过；Offline 378 通过、25 按依赖条件跳过，另有真实认证 Redis 单点/Cluster 及完整 Dashboard 正常/故障 E2E。前端重新导出真实新鲜 SEO 数据后全量单测与完整构建通过。此前工作区测试数量包含未发布的并行修改，不能替代该固定提交记录。

受控备份与验收目录：ca1 `/home/ubuntu/services-deploy/.runtime/ca1-rec-ids-20261115/`，保留 `sar.cutover.dump`、原配置、SQLite/行为备份、镜像清单及验收日志。若回退，必须恢复同套旧程序、配置和对应数据备份；不兼容混用旧格式。
