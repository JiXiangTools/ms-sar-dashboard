# ms-sar-dashboard

`ms-sar-dashboard` 是搜广推后台管理服务，使用 Go 实现。它承接原来 `ms-data-receiver` 中的管理端登录、应用授权配置管理和后台页面能力，并参考 `ms-user-center` 的数据库、JWT、中间件、仓储、服务和后台 UI 分层。

目标边界：

- 登录、管理员账号、应用授权配置全部归属本项目，并通过数据库持久化。
- 授权应用统一供 `ms-data-receiver`、`ms-rec-online`、`ms-search-online` 使用。
- 三个在线服务统一使用请求头 `x-dwzauth-appid`、`x-dwzauth-secret`、`x-request-id`，并到 Redis `app_auth_{appid}` 校验 `appid + secret`。
- `ms-data-receiver` 不再保存管理员账号、登录配置或应用配置文件，只在客户端上报时使用授权信息。
- 客户端上报链路仍由 `ms-data-receiver` 负责：校验授权、校验商品/行为数据、写 Kafka。
- 后台管理链路由 `ms-sar-dashboard` 负责：登录、token、管理员、应用授权配置、审计和后续搜广推运营管理页面。

## 首期功能

- 账号密码登录，登录成功和失败都必须记录日志。
- 授权应用管理：新增、删除、修改、分页列表。

## 文档导航

- [docs/design.md](./docs/design.md)：总体设计、职责边界、数据模型和认证授权方案
- [docs/api-admin.md](./docs/api-admin.md)：管理端 API 契约
- [docs/migration.md](./docs/migration.md)：从 `ms-data-receiver` 迁移登录和授权配置的实施计划
- [docs/agent.md](./docs/agent.md)：协作约束和后续实现注意事项

## 参考项目

- `../ms-data-receiver`：现有数据接入服务，当前包含配置式管理员登录和本地 `data/apps.json` 应用授权配置。
- `../ms-rec-online`：在线推荐服务，后续改为使用 dashboard 统一应用授权。
- `../ms-search-online`：在线搜索服务，后续改为使用 dashboard 统一应用授权。
- `../ms-user-center`：目标分层参考，包含 PostgreSQL、Redis、JWT、管理员登录、应用授权、审计日志、后台 UI 和统一响应模式。

## 当前阶段

当前仓库先形成文档和实现边界，后续再按文档落地 Go 工程、数据库脚本、接口、后台 UI，以及 `ms-data-receiver`、`ms-rec-online`、`ms-search-online` 侧授权改造。
