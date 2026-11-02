# 应用数据统计

## 职责与链路

`ms-data-receiver → Kafka → alg-rec-offline raw → rec-statistics → Dashboard 内部 HTTP 接口 → PostgreSQL 汇总表 → 管理端查询与图表`。

Offline 负责全部业务聚合和去重，只向 Dashboard 发送计数，不发送用户 ID、事件 ID、交易 ID或原始商品/行为。Offline 不连接 Dashboard 数据库、不启动 HTTP 服务。Dashboard 负责校验、持久化、版本控制、管理员查询及展示。

## 指标口径 v1

- `appid` 是应用命名空间，所有数据独立计算；统计时区固定 `Asia/Shanghai`。
- 商品上报记录按 `timestamp` 计数。普通商品按 `appid + item_id` 去重，最新状态沿用 Offline 的 generation/topic/partition/offset 和 legacy version 比较规则；`disabled=false` 计入当前可用商品。广告不计入普通商品库存。
- `item_total` 是完整可用 raw 历史中的去重商品总数；`item_available` 是最新快照可用数量。这两个是当前库存快照，不表示所选时间段内的值。`history_since` 明示可回算历史起点。
- `item_reports` 是商品快照记录数，重复更新也计数；Kafka 同一来源重放不重复计数。`new_items` 按商品首次上报时间归属；`reported_items` 是每个小时或每天内上报的去重商品数，不能把多个时间段的这个值相加当作期间去重数。
- 行为按发生时间 `action_at` 分桶，而非 raw 文件的接入时间分区；迟到上报在下一次重算时修正原发生日期。
- 原始事件逐类计数，`PAGE_VIEW` 为页面浏览，`ITEM_DETAIL_VIEW` 为详情浏览，`ITEM_OPEN`、内容进度等单列。不能把推荐侧 `vw_*` 集合大小当浏览次数。
- 普通商品 CTR = `clicks / exposures`。仅有 `item_id`、对象为 ITEM（兼容缺省对象）且非广告的 `CLICK`/`SHOW` 参与。按钮、搜索、广告事件不混入。跨日期汇总后再相除，曝光为零返回 `null`。这是事件次数比率，无曝光关联的重复真实点击可能使 CTR 超过 100%，不截断数值；不据此宣称推荐归因效果。
- 原始事件计数保留 `search_result_*`、`ad_*` 等类型供独立选择查看。`PURCHASE` 表示购买事件次数，现有上报不保证支付成功或订单级唯一，因此页面不称其为成功订单数或收入。
- 新增 `TIP` 表示一次成功打赏。必须在确认支付成功后上报，`event_id` App 内稳定唯一且重试保持不变；`target_type` 为 ITEM 或 AUTHOR，`target_id` 必填。ITEM 时 `item_id` 必填且等于 target_id。`properties.transaction_id` 是 App 内唯一打赏交易 ID，`amount_minor` 是大于零且不超过 10^12 的整数最小货币单位金额，`currency` 是三个大写英文字母的币种代码。示例 CNY 100 表示人民币 1 元。字段放在现有 properties 扩展内，Kafka 和 raw 列数不变。第一期只展示打赏次数，不混算不同币种金额。
- 行为先按 Kafka 来源去重，再按 App 内 `event_id` 去除跨请求重试；TIP 额外使用交易 ID 去重。缺少 event_id 的历史行为只能去除同一 Kafka 来源重放，真实多次行为全部保留，缺失数量通过 `missing_event_ids` 展示。`duplicate_events` 是去掉的跨消息业务重复数量。现有推荐榜单的计分规则保持原契约。

## 内部写入契约

`PUT /api/v1/internal/statistics/apps/:appid/days/:date`

- 仅供 Offline 调用，Header 为 `Authorization: Bearer <MSSAR_STATISTICS_INGEST_TOKEN>`。独立于管理员 JWT 和 App Secret；未配置则返回 503，错误凭据返回 401。生产环境经内网或 HTTPS 访问，网关不要公开内部路径。
- 每次写一个 App 的完整自然日快照，包括 24 个小时和日汇总；默认全局请求体上限 1 MiB，每来源 600 次/分钟。大批量回算客户端须遵守 `Retry-After`。
- `schema_version=1`；`appid` 是规范十进制字符串；`date` 为 YYYY-MM-DD；`timezone=Asia/Shanghai`；`revision` 是正整数且不超过 JavaScript 安全整数上限；`generated_at` 为统计截点的秒级时间戳。
- `counts` 含 `item_reports,new_items,exposures,clicks,missing_event_ids,duplicate_events,events`，其中 events 为小写事件名到非负次数的映射。所有可加计数必须等于 24 小时合计。
- `hours` 按 0..23 排序，每项含 `hour,reported_items,counts`；日级独立 `reported_items` 计数不能用小时去重数相加替代。
- `snapshot` 含 `generated_at,history_since,item_total,item_available`。
- 返回统一 envelope：`data={"changed":true|false,"revision":...}`。同版本同内容重试返回 changed=false；同版本不同内容、旧版本均为 409；无应用为 404，非法输入为 400。
- PostgreSQL 在事务内替换整天数据并更新最新商品快照。主键为 `(appid,day)`，防止重复累加；首次上报记录不足或未同步成功不会伪造统计行。
- 各天独立提交；若批次部分上传失败，已完成天仍可查询，失败天保留上次结果或显示未就绪，各天携带各自计算时间。客户端对网络/429/5xx 最多重试 3 次，再交给调度器重跑；不把失败当成功。

## 管理端查询

`GET /api/v1/admin/statistics?appid=100001&from=2026-11-01&to=2026-11-02&granularity=day`

需要管理员 JWT。from/to 是北京时间日期，包含两端，最多 90 天；granularity 可为 day/hour。结果包含 snapshot、summary、CTR、points、missing_buckets、stale。每个 point 含 time、counts、reported_items、ctr、generated_at、partial。

`counts=null` 表示未统计，不是零；未来时间段同样不补零。已处理且没有事件的时间段返回零。曝光零时 ctr=null；当前小时/天 partial=true。30 分钟未刷新显示过期提示。期间汇总包含已知时间段，页面同时提示缺失段数。管理页面和资源 noindex，不加入 sitemap。

## 部署与回算

1. 先部署支持 TIP 的 ms-data-receiver，业务方在成功打赏后按上述契约上报。
2. 使用 `ms-sar-dbtool` 执行迁移 000002；运行账户只需两张统计表的 SELECT/INSERT/UPDATE 权限，DDL 仍由迁移账户执行。原始数据不落 Dashboard 数据库。
3. 在 Dashboard 配置 `MSSAR_STATISTICS_INGEST_TOKEN`，Offline 设置同值 `AROF_STATISTICS_INGEST_TOKEN`，通过秘密管理系统注入，勿提交仓库。
4. Offline 设置 `AROF_STATISTICS_DASHBOARD_URL=http://ms-sar-dashboard:8081` 和现有 `AROF_SAR_DASHBOARD_AUTH_APP_URL=http://ms-sar-dashboard:8081/api/v1/auth/app`。运行 `bash admin/start.sh rec-statistics --env prod`，默认每次成功后间隔 600 秒；单次回算使用 `RUN_INTERVAL_SECONDS=0`。
5. 统计实例挂载完整 raw 数据卷和持久 state 卷，与 consumers 使用同一份数据。必须存在 item/behavior 根目录；缺少挂载、已发布历史文件丢失/缩短、损坏行会阻止覆盖旧统计。首次运行只能基于现存 raw 声明历史，历史不完整应先恢复备份。
6. v1 每次用磁盘临时 SQLite 聚合完整 raw 历史，内存只保留汇总，默认发布最近 90 天（`AROF_STATISTICS_DAYS=1..90`）。临时数据库仅在 Offline 本地用于计算，成功或异常退出时清理；不向 Dashboard 暴露。应监控磁盘空间和运行时长；数据规模增长时演进为增量任务。
7. `AROF_APPID` 可指定单 App。禁止给统计任务传 AROF_DATE/HOUR/START_DATE/END_DATE，避免截断历史后覆盖库存。所有副本共用同一 state 卷，并用文件锁防止并行任务；revision 与上次成功 raw 清单持久化在 state/statistics。
8. 前台「数据统计」选择 App、7/30 天或自定义区间、小时/天粒度。检查商品卡片、CTR、浏览、打赏购买及其他事件趋势。查看数据明细可获得精确值。

## 验证

- Dashboard：`go test ./...`；设置专用 `MSSAR_STATISTICS_TEST_DSN` 和 `MSSAR_MIGRATION_TEST_DSN` 后执行真实 PostgreSQL 快照和迁移测试。
- Offline：`python -m pytest -q`，统计测试覆盖跨请求/跨交易/Kafka 去重、商品更新和禁用、迟到事件、缺失事件 ID、零值与未来事件、历史缺失防护。
- Receiver：`go test ./...`，包括 TIP 必填字段、金额单位、币种和目标校验。
- `test/test-action-upload.py --dry-run` 生成当前全部行为枚举的合法样例，正式上报需要显式 DWZ_APP_SECRET。
