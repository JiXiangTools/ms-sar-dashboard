# Agent 协作约束

本文面向后续参与 `ms-sar-dashboard` 的自动化 agent 和辅助开发工具。业务设计以 [design.md](./design.md) 为准，接口契约以 [api-admin.md](./api-admin.md) 为准，迁移步骤以 [README.md](../README.md) 为准。

## 1. 不可破坏的边界

- 登录、管理员账号、应用授权配置归属 `ms-sar-dashboard`。
- 首期必须支持账号密码登录，并记录登录成功和失败日志。
- 首期必须支持授权应用新增、删除、修改和分页列表。
- `ms-data-receiver`、`ms-rec-online`、`ms-search-online` 都使用 dashboard 配置的统一应用授权。
- 三个在线服务授权 Header 固定为 `x-dwzauth-appid`、`x-dwzauth-secret`、`x-request-id`。
- 三个在线服务必须调用 dashboard `POST /api/v1/auth/app` 校验 `appid + secret`。
- 禁止继续使用固定 `x-dwz-auth` 或本地静态 token 作为线上授权。
- 管理端鉴权固定为 `Authorization: Bearer <jwt>`。
- 管理员密码必须使用 bcrypt hash。
- 不在配置文件中恢复 `admin.name` 和 `admin.password`。
- 不在 `t_admin` 中增加 token 存储字段。
- 不把客户端商品/行为上报数据写入 dashboard 数据库作为最终输出。
- 不改变 `ms-data-receiver` 商品/行为上报字段、Kafka 消息格式和下游离线契约。

## 2. 工程规则

- 参考 `ms-user-center` 的分层，不在 handler 中堆 SQL、缓存和复杂业务逻辑。
- repository 负责数据库访问。
- service 负责业务校验、缓存同步、审计和 token 逻辑。
- middleware 负责 request_id、访问日志、错误日志、恢复和认证。
- 管理端写操作必须写入 `t_admin_log`。
- 登录成功和失败必须写入 `t_admin_log`，不得只写访问日志。
- 写操作影响授权缓存时，必须同步更新或删除 dashboard 内部 Redis 授权投影 `app_auth_{appid}`。
- 响应中不得返回管理员密码。
- 日志和审计中不得记录 password、secret、token 明文。

## 3. 修改文档同步要求

修改业务边界时必须同步：

- [design.md](./design.md)
- [README.md](../README.md)

修改接口时必须同步：

- [api-admin.md](./api-admin.md)
- handler / service / repository
- API 契约测试

修改数据库结构时必须同步：

- `upgrade/sql/schema.sql`
- `upgrade/sql/data.sql`
- [design.md](./design.md)
- repository 测试

改造 `ms-data-receiver`、`ms-rec-online`、`ms-search-online` 授权时必须同步检查：

- `configs/*.yaml`
- `internal/config`
- 认证中间件或 handler 中的授权读取逻辑
- dashboard 授权 API 客户端模块
- `docs/design.md`
- `docs/api.md`
- `docs/admin-api.md` 是否需要废弃或迁移说明
- 是否仍残留 `x-dwz-auth`、`auth.x_dwz_auth`、固定 `app.appid` 作为线上授权来源

## 4. 最低测试要求

每次实现变更至少覆盖相关测试：

- 配置加载。
- 管理员登录和 token 校验。
- 登录成功和失败日志。
- 管理员禁用或密码更新后 token 失效。
- 应用授权配置新增、分页列表、修改、删除。
- dashboard 内部 Redis 授权投影 `app_auth_{appid}` 同步。
- 应用删除后三个在线服务授权失败。
- 缓存同步。
- data-receiver 客户端上报授权成功和失败。
- rec-online 推荐接口统一 Header 授权成功和失败。
- search-online 搜索接口统一 Header 授权成功和失败。

如果暂时无法补测试，交付说明必须明确测试缺口和风险。

## 5. 交付说明建议

每次交付说明建议包含：

- 改了什么。
- 为什么改。
- 涉及哪些接口、SQL、缓存、日志和 UI。
- 已验证什么。
- 是否影响 `ms-data-receiver` 客户端上报、`ms-rec-online` 推荐、`ms-search-online` 搜索兼容性。
- 还有哪些风险或后续项。

## 6. 通用 AI Agent 12 条规则 后续 AI Agent 工作必须严格遵循。

1. **先思考再编码**：动手前明确假设、未知点和权衡；不确定时先问，不靠猜；若有更简单路径，要主动指出。
2. **简洁优先**：只实现当前目标需要的最小改动；不做投机功能；不为一次性逻辑抽象；发现过度设计要收敛。
3. **外科手术式修改**：只改必须改的文件和代码；不顺手重排、重构、改注释或格式；保持现有风格。
4. **目标驱动执行**：先定义成功标准，再围绕标准迭代验证；能少步骤完成就少步骤完成。
5. **确定性逻辑写成代码**：重试、路由、阈值、升级策略等稳定规则必须落到显式代码、配置或查找表；模型只处理语言、分类、摘要、草稿和歧义消解。
6. **设置硬性预算**：调试、重构、生成循环要有最大轮次、耗时或 token 预算；预算用尽立即停下并汇报当前状态；被否决的方案不要反复提出。
7. **暴露冲突而不是折中混合**：遇到两套互相矛盾的模式，明确指出冲突并请求决策；不要自行混搭或擅自选择。
8. **先读再写**：新增实现前必须阅读当前文件及相关导入，查找同类函数、工具、常量和模式；已有可复用实现时不得重复造。
9. **测试验证意图**：测试要验证值、结构、副作用、错误类型等有意义行为；“不报错”不等于正确；测试薄弱时必须说明。
10. **长任务设检查点**：超过 3 步或触及超过 3 个文件时，每步记录做了什么、改了什么、当前状态；失败时回到上一个可靠状态；思路丢失时停止并重述。
11. **惯例优先于新颖**：遵守仓库既有命名、架构和分层；不要引入第二套模式；若确需改变惯例，先提出方案并等待确认。
12. **失败显性化**：错误必须抛出、返回或上报；批处理、迁移、循环跳过数据时必须展示数量和原因；无法 100% 确认成功时必须明确说明。
