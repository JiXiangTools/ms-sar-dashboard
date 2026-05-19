# Agent 协作约束

本文面向后续参与 `ms-sar-dashboard` 的自动化 agent 和辅助开发工具。业务设计以 [design.md](./design.md) 为准，接口契约以 [api-admin.md](./api-admin.md) 为准，迁移步骤以 [README.md](../README.md) 为准。

## 1. 不可破坏的边界

- 登录、管理员账号、应用授权配置归属 `ms-sar-dashboard`。
- 首期必须支持账号密码登录，并记录登录成功和失败日志。
- 首期必须支持授权应用新增、删除、修改和分页列表。
- `ms-data-receiver`、`ms-rec-online`、`ms-search-online` 都使用 dashboard 配置的统一应用授权。
- 三个在线服务授权 Header 固定为 `x-dwzauth-appid`、`x-dwzauth-secret`、`x-request-id`。
- 三个在线服务必须到 Redis `app_auth_{appid}` 校验 `appid + secret`。
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
- 写操作影响授权缓存时，必须同步更新或删除 Redis `app_auth_{appid}`。
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
- Redis 授权读取模块
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
- Redis `app_auth_{appid}` 同步。
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
