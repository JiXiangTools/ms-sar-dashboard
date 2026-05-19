# 迁移计划

## 1. 目标状态

- dashboard 管理员登录依赖数据库。
- dashboard 管理应用授权，并同步 Redis `app_auth_{appid}`。
- `ms-data-receiver`、`ms-rec-online`、`ms-search-online` 都使用：
  - `x-dwzauth-appid`
  - `x-dwzauth-secret`
  - `x-request-id`
- 三个在线服务都只从 Redis 校验授权。
- 旧 `data/apps.json`、固定 `x-dwz-auth`、固定 `app.appid` 不再作为线上授权来源。

## 2. 当前状态

| 服务 | 当前授权 | 目标授权 |
| --- | --- | --- |
| `ms-data-receiver` | `data/apps.json` + `x-dwzauth-appid/secret` | Redis `app_auth_{appid}` + 统一 Header |
| `ms-rec-online` | 固定 `x-dwz-auth`，appid 来自配置 | Redis `app_auth_{appid}` + 统一 Header |
| `ms-search-online` | 固定 `x-dwz-auth` / allowed appids，请求体 appid | Redis `app_auth_{appid}` + 统一 Header |

## 3. 实施阶段

### 阶段 1：dashboard 基础工程

- 配置加载。
- PostgreSQL 客户端。
- Redis 客户端。
- 统一响应。
- request_id。
- 访问日志和错误日志。
- 健康检查。

验收：

- `/health` 可用。
- 数据库和 Redis 健康检查可用。

### 阶段 2：数据库与初始化

- 创建 `t_admin`。
- 创建 `t_app`。
- 创建 `t_admin_log`。
- 创建 `t_app_id_seq START WITH 100001`。
- 提供 `cmd/hash-password`。
- 初始化管理员时显式传入 bcrypt hash。

验收：

- 初始管理员可登录。
- SQL 文件不包含明文生产密码。

### 阶段 3：登录

- 实现账号密码登录。
- 签发 access token。
- 登录成功写 `LOGIN_SUCCESS`。
- 登录失败写 `LOGIN_FAILED`。
- 不实现 refresh token。

验收：

- 正确账号可登录。
- 错误密码不可登录。
- 登录日志完整且无密码/token。

### 阶段 4：应用管理与 Redis 同步

- 应用分页列表。
- 创建应用。
- 修改应用。
- 删除应用。
- 同步 Redis `app_auth_{appid}`。

验收：

- 创建应用后 Redis 有 `app_auth_{appid}`。
- 修改 secret 后 Redis 立即更新。
- 删除应用后 Redis key 被删除。
- Redis 同步失败时管理端操作失败。

### 阶段 5：迁移历史 apps.json

- 读取 `ms-data-receiver/data/apps.json`。
- 保留原 `appid` 和 `secret`。
- 写入 `t_app`。
- 同步 Redis。
- 调整 appid 序列到历史最大值之后。

映射：

| apps.json | t_app |
| --- | --- |
| `appid` | `id` |
| `name` | `name` |
| `secret` | `secret` |
| `disabled` | `disabled` |
| `remark` | `remark` |
| `update_time` | `last_update_time` |

验收：

- 历史 appid/secret 可继续授权。
- 迁移报告列出非法或重复数据。

### 阶段 6：改造 ms-data-receiver

- 删除管理端登录和应用管理接口。
- 删除本地授权文件作为主存储。
- 使用 Redis `app_auth_{appid}` 校验授权。
- 保持商品/行为上报 API 和 Kafka 消息格式不变。

验收：

- 统一 Header 授权成功。
- secret 错误、应用删除、Redis 不可用时不放行。

### 阶段 7：改造 ms-rec-online

- 移除固定 `x-dwz-auth` 作为线上授权。
- appid 改为来自 `x-dwzauth-appid`。
- 使用 Redis `app_auth_{appid}` 校验授权。
- 推荐 Redis key 使用 Header appid。

验收：

- 统一 Header 授权成功。
- 推荐结果格式不变。
- Redis key 使用 Header appid。

### 阶段 8：改造 ms-search-online

- 移除固定 `x-dwz-auth` 作为线上授权。
- appid 改为来自 `x-dwzauth-appid`。
- 使用 Redis `app_auth_{appid}` 校验授权。
- ES 索引使用 Header appid 路由。
- 若暂时保留请求体 appid，必须与 Header appid 一致。

验收：

- 统一 Header 授权成功。
- ES 索引路由正确。
- 请求体 appid 与 Header appid 不一致时拒绝。

### 阶段 9：Debug 页面

- ES Debug：索引、mapping、settings、count、doc、只读 search。
- 推荐 Debug：读取和解析推荐 Redis key。
- 所有 debug 操作写审计日志。

验收：

- Debug 页面只读。
- 不暴露 ES/Redis 密码。
- 操作有审计。

## 4. 切换顺序

1. 部署 dashboard 数据库和 Redis 配置。
2. 初始化管理员。
3. 迁移 `data/apps.json`。
4. 校验 Redis `app_auth_{appid}`。
5. 切换 `ms-data-receiver`。
6. 切换 `ms-rec-online`。
7. 切换 `ms-search-online`。
8. 验证上报、推荐、搜索。
9. 下线旧管理端和旧固定 token。

## 5. 回滚

- 切换前备份 `data/apps.json`。
- 保留三个在线服务旧版本镜像。
- 回滚期间禁止同时在旧 data-receiver 后台和 dashboard 修改应用。
- 如果 Redis 授权异常，回滚在线服务到旧授权版本。

## 6. 测试清单

dashboard：

- 登录成功和失败。
- 登录日志。
- 应用创建、分页、修改、删除。
- Redis 同步。
- Redis 同步失败回滚或返回失败。
- 审计日志脱敏。

data-receiver：

- 统一 Header 授权成功。
- secret 错误失败。
- 应用删除失败。
- Redis 不可用失败。
- Kafka 消息格式不变。

rec-online：

- 统一 Header 授权成功。
- 推荐 Redis key 使用 Header appid。
- 推荐响应格式不变。

search-online：

- 统一 Header 授权成功。
- ES 索引使用 Header appid。
- 搜索响应格式不变。

Debug：

- ES Debug 只读。
- 推荐 Debug 只读。
- Debug 审计日志。
