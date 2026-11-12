# 集中系统告警

Dashboard、ms-rec-online、ms-search-online、ms-data-receiver 和 alg-rec-offline 使用同一告警契约。业务服务只负责异步入队，Dashboard 每 5 秒消费并向飞书自定义机器人发送。WebHook 不下发到生产者，无需应用授权或签名。

## Redis 与消息契约

所有服务必须连接同一环境的共享 Redis、同一个 DB 和 key_prefix；Cluster 使用 DB 0。队列为 Redis Stream `<key_prefix>system-alert`，每条消息只有 `alert` 字段，值为 JSON：

```json
{
  "schema_version": 1,
  "alert_id": "a_5a369a3d4f614ad7afbe1c5a06dd99bd",
  "level": "ERROR",
  "reason": "Redis unavailable",
  "request_id": "rq_12",
  "location": "internal/service/recommend/experiment.go:280",
  "params": {"appid": "100001"},
  "created_at": 1794484800,
  "module": "ms-rec-online",
  "event": "recommend.decision_publish_failed",
  "env": "test",
  "instance_id": "rec-online-01"
}
```

- ERROR、CRITICAL、FATAL、PANIC 可入队；DEBUG、INFO、WARN 不告警。发送端还会独立检查级别。
- created_at 为秒级 Unix 时间戳，飞书通知按 Asia/Shanghai 显示；后台任务的 request_id 可以为空。
- event 是稳定错误类型，location 是发生位置；Go 从调用栈记录业务文件和行号，无法定位时使用日志 event；Python 使用模块、函数和行号。Python 异常日志的 reason 同时包含异常类型、实际原因和日志消息，params.error 保留异常摘要；两者继续脱敏并限制长度，不采集完整堆栈或局部变量。
- params 使用受控参数白名单，包括 appid、状态、操作、策略/实验版本、stream_id、error 等；不采集请求体、用户 ID、认证 Header、完整 IP 或 User-Agent。原因和参数同时脱敏凭据、截断长度。单条有效载荷最多 8 KiB。
- alert_id 在写入重试中保持不变。跨重试可能产生重复记录，不承诺通知严格只发送一次。

## 生产者与容量边界

开启后，ERROR 日志只进入本机 1024 条有界队列。独立后台 worker 使用独立 Redis 连接池，每批最多 100 条，单次 Redis 操作限时 300ms，失败最多尝试 3 次。正常退出最多等待 2 秒排空本机队列；ms-rec-online 的启动失败、关闭失败等受控错误退出同样执行排空，再以非零状态退出。Python 在后台首次使用时才初始化独立 Redis client，告警不让原本不依赖 Redis 的任务在请求或任务处理线程中等待连接。

本机队列满或重试失败时，保留原有业务错误日志，记录 alerts_dropped / alert_enqueue_failed 和丢弃数量。告警线程自身的错误直接输出本地日志，不再次入队。业务请求不等待飞书，也不等待告警 Redis I/O。

主 Stream 和隔离 Stream 各最多 10000 条，Lua 原子检查容量后追加。达到容量时拒绝新增并记录丢弃数量，不裁剪已有未确认消息。队列没有 TTL。该链路为优先保证业务可用的尽力投递：进程异常退出可能丢失本机尚未写入 Redis 的消息；网络结果不明确时可能重复。Redis 内消息的故障持久性仍取决于部署的 AOF、复制及淘汰策略。

## Dashboard 消费

- 独立后台任务每 5 秒轮询；使用 `dashboard-system-alert-v1` 消费组和每实例唯一 consumer 名称。读取自身 pending、新消息，并接管空闲超过 60 秒的遗留 pending。要求 Redis 6.2 或更高版本。
- 每轮有界读取，按 env/module/level/event 聚合。通知带发生次数、首次/最后时间、样例 request_id、原因、位置、参数和告警 ID；每次最多发送 10 组，并优先处理旧告警。
- 通过共享 `<prefix>system-alert:send-slot` 限速，多 Dashboard 合计最多每秒一次。飞书请求超时 3 秒，拒绝重定向，响应最多读取 4 KiB。消息体小于 19 KiB。
- HTTP 200 且飞书业务 code=0 才算成功；兼容旧 StatusCode=0。成功后单 Key Lua 执行 XACK + XDEL，不提前确认。
- 失败保留 pending，退避 5、10、20 秒，最长 5 分钟；重试状态保存在 `<prefix>system-alert:retry` Hash，实例切换后继续。有效告警发送失败累计 10 次后，直接 XACK + XDEL 并清除重试状态，本地记录丢弃数量、stream_id 和原因；不进入隔离队列，渠道恢复后不再补发。
- 非法消息或重试状态损坏的记录，先写 `<prefix>system-alert:quarantine` Stream，保留 stream_id、原 fields、原因和隔离时间，成功后才确认原消息并清除其重试状态。隔离失败则保留原 pending，其他合法告警仍可处理。Redis 连接等基础设施错误仍按消费失败处理。
- 隔离队列仅保存需要排查的坏记录，由运维核对并清理；修复来源或重试状态后，可按原 alert_id 将合法 JSON 重投主队列。不要直接删除未确认主队列消息。

## 环境配置

各服务均支持 `alert.enabled`，dev/prod 默认 false，test 为 true。飞书地址只放在 Dashboard 的 `alert.feishu_webhook_url`；测试配置已填入指定测试机器人，生产留空。Dashboard 未配置 WebHook 时不启动发送；生产者也应保持关闭，避免无人消费的告警积压。

```yaml
alert:
  enabled: false
  # 仅 Dashboard；生产通过环境变量注入。
  feishu_webhook_url: ""
```

| 服务 | 开关环境变量 | Redis 配置 |
|---|---|---|
| Dashboard | MSSAR_ALERT_ENABLED | 复用 MSSAR_REDIS_* |
| 推荐 | MSRO_ALERT_ENABLED | 复用 MSRO_REDIS_* |
| 搜索 | MSO_ALERT_ENABLED | MSO_REDIS_MODE / ADDRS / USERNAME / PASSWORD / DB / KEY_PREFIX 等 |
| 数据接入 | MSDR_ALERT_ENABLED | 复用 MSDR_REDIS_*，新增 KEY_PREFIX |
| 离线 | AROF_ALERT_ENABLED | 复用 AROF_REDIS_* |

Dashboard 的机器人变量为 `MSSAR_ALERT_FEISHU_WEBHOOK_URL`。搜索仅在启用告警时连接 Redis，ES 检索和鉴权流程不依赖该连接。各环境应使用隔离 Redis 或一致配置的不同 key_prefix，避免测试机器人消费生产告警。

查看积压：`XLEN system-alert`、`XPENDING system-alert dashboard-system-alert-v1`；查看隔离项：`XRANGE system-alert:quarantine - + COUNT 20`。排查时同时查看 alerts_dropped 日志。

协议参考：[Redis 消费组](https://redis.io/docs/latest/commands/xreadgroup/)、[pending 接管](https://redis.io/docs/latest/commands/xautoclaim/)、[飞书自定义机器人](https://open.feishu.cn/document/client-docs/bot-v3/add-custom-bot?lang=zh-CN)。飞书机器人限额为每秒 5 次、每分钟 100 次，本实现使用更低的统一发送速率。
