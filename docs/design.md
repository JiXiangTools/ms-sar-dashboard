# ms-sar-dashboard 设计文档

## 1. 设计原则

按第一性原理，本项目只需要解决四件事：

- 让管理员登录 dashboard，并留下登录日志。
- 让管理员维护应用授权信息。
- 让 `ms-data-receiver`、`ms-rec-online`、`ms-search-online` 用同一套授权方式访问。
- 给搜索和推荐链路提供只读 debug 页面。

按奥卡姆剃刀，首期只保留一个事实源、一个在线授权入口、两种身份：

- PostgreSQL 是管理端事实源：管理员、应用、审计日志都在数据库。
- dashboard 授权 API 是在线服务唯一授权入口；dashboard 内部维护 Redis 授权投影 `app_auth_{appid}` 和 `app_auth_allappids`。
- 管理端身份使用账号密码登录后签发短期 JWT。
- 可选接入 `ms-user-center` 的 CAS 单点登录；CAS 换票成功后仍由 dashboard 签发本地短期 JWT。
- 在线服务身份使用请求头 `x-dwzauth-appid` + `x-dwzauth-secret`。

不做多套授权来源，不做 dashboard 到在线服务的鉴权代理，不在首期做管理员管理 UI，不在 debug 页面写 ES 或 Redis。

接口路径、字段和响应示例见 [api-admin.md](./api-admin.md)。迁移和切换步骤见 [README.md](../README.md)。

## 2. 目标

首期目标：

- 通过账号密码登录 dashboard。
- 可选通过 `ms-user-center` 单点登录进入 dashboard。
- 登录成功和失败都记录到审计日志。
- 新增、修改、删除授权应用。
- 分页查询授权应用列表。
- 授权应用同步到 Redis。
- `ms-data-receiver`、`ms-rec-online`、`ms-search-online` 统一通过请求头携带：
  - `x-dwzauth-appid`
  - `x-dwzauth-secret`
  - `x-request-id`
- 三个在线服务都调用 dashboard `POST /api/v1/auth/app` 校验 `appid + secret`。
- 提供只读 ES debug 页面，用于查看索引、mapping、文档和执行受控查询。
- 提供只读推荐 debug 页面，用于调用推荐在线接口并查看返回结果。

非目标：

- 不改变 `ms-data-receiver` 商品/行为上报字段、Kafka 消息格式和下游契约。
- 不改变 `ms-rec-online` 推荐结果格式。
- 不改变 `ms-search-online` ES 索引字段语义和搜索响应结构。
- 不把客户端上报数据写入 dashboard 数据库。
- 不在数据库或 Redis 中保存管理端 token。
- 不提供 ES 写入、删除、重建索引能力。
- 不提供推荐 Redis 写入或删除能力。

## 3. 总体架构

```text
                         +------------------+
管理员浏览器  ----------> | ms-sar-dashboard |
                         +------------------+
                           | optional CAS
                           |   /api/v1/admin/cas/admin
                           v
                        ms-user-center
                           ^
                           |
                     /uc-admin?...
                           |
                           v
                           | PostgreSQL
                           |   t_admin
                           |   t_app
                           |   t_admin_log
                           v
                           | Redis app_auth_{appid}
                           | Redis app_auth_allappids
                           |
                           v
                  POST /api/v1/auth/app
                           ^
                           |
        +------------------+------------------+
        |                  |                  |
ms-data-receiver     ms-rec-online      ms-search-online
```

数据流：

1. 管理员在 dashboard 创建或修改应用。
2. dashboard 写 PostgreSQL。
3. dashboard 同步内部 Redis 授权投影 `app_auth_{appid}` 和 `app_auth_allappids`。
4. 三个在线服务收到请求后，调用 dashboard 授权接口校验。

单点登录数据流：

1. 管理员在 dashboard 登录框点击“单点登录”。
2. dashboard 服务端生成 `ms-user-center /uc-admin` 跳转地址，并计算 CAS `appsecret` 签名。
3. `ms-user-center` 校验应用信息，完成管理员身份确认后，把 `token=<cas_token>` 带回 dashboard 的 `redirect_url`。
4. dashboard 服务端使用 CAS token 调用 `ms-user-center /api/v1/admin/cas/admin` 查询管理员信息。
5. 查询成功后，dashboard 本地签发管理端 JWT，后续请求继续使用本地 `Authorization: Bearer <jwt>`。

核心约束：

- PostgreSQL 是后台管理事实源。
- dashboard 授权接口是在线服务唯一授权入口。
- 在线服务不读 `t_app`，不读 Redis 授权投影，不读本地授权文件。
- dashboard 授权 API 自身不做降级；在线服务在授权服务异常时只能按各自文档化的本地成功授权缓存策略处理。

## 4. 职责边界

### 4.1 ms-sar-dashboard

负责：

- 管理员账号密码登录。
- 对接 `ms-user-center` 单点登录。
- 签发管理端 JWT access token。
- 记录登录成功、登录失败、应用变更和 debug 操作日志。
- 应用授权配置增删改查。
- 将应用授权同步到 Redis。
- 提供有效应用 appid 列表查询。
- 提供 ES debug 页面。
- 提供推荐 debug 页面。

不负责：

- 客户端数据上报。
- 搜索在线查询。
- 推荐在线计算。
- 生成 ES 索引。
- 写推荐 Redis。

### 4.2 ms-data-receiver

负责：

- 商品/内容上报。
- 用户行为上报。
- 使用统一 Header 调用 dashboard 授权接口校验授权。
- 校验请求并写 Kafka。

移除：

- 管理员登录接口。
- 管理端应用配置接口。
- `/dr-admin` 页面。
- 本地 `data/apps.json` 作为授权主存储。

### 4.3 ms-rec-online

负责：

- 个性化推荐、热门推荐、相关推荐。
- 使用统一 Header 调用 dashboard 授权接口校验授权。
- 使用 Header `x-dwzauth-appid`（规范十进制字符串）作为推荐 Redis key 中的 `{appid}`。

移除：

- 固定 Header `x-dwz-auth` 作为线上授权。
- 固定配置项 `app.appid` 作为线上请求 appid 来源。

### 4.4 ms-search-online

负责：

- 搜索查询、分页、过滤、排序、高亮。
- 使用统一 Header 调用 dashboard 授权接口校验授权。
- 使用 Header `x-dwzauth-appid`（规范十进制字符串）路由到 ES 索引。

移除：

- 固定 Header `x-dwz-auth` 作为线上授权。
- `search.allowed_appids` 作为授权来源。
- 请求体 `appid` 作为授权来源。若短期兼容请求体 `appid`，必须与 Header appid 一致。

## 5. 配置

环境变量前缀建议使用 `MSSAR`。

```yaml
app:
  name: ms-sar-dashboard
  env: dev
  host: 0.0.0.0
  port: 8081
  read_timeout: 5s
  write_timeout: 10s
  shutdown_timeout: 10s

auth:
  jwt_secret: ms-sar-dashboard-dev-secret
  access_token_ttl: 2h
  issuer: ms-sar-dashboard

sso:
  enabled: false
  admin_ui_url: https://uc.example.com/uc-admin
  api_base_url: https://uc.example.com
  app_id: "100001"
  app_secret: "secret-from-user-center"
  redirect_url: https://sar.example.com/sar-admin
  request_timeout: 3s

database:
  dsn: postgres://postgres:postgres@127.0.0.1:5432/ms_sar_dashboard?sslmode=disable
  max_open_conns: 20
  max_idle_conns: 5
  conn_max_lifetime: 30m
  health_check_timeout: 2s

redis:
  mode: standalone
  addrs:
    - 127.0.0.1:6379
  key_prefix: ""
  db: 0
  dial_timeout: 2s
  read_timeout: 2s
  write_timeout: 2s
  health_check_timeout: 2s

elasticsearch:
  addrs:
    - http://127.0.0.1:9200
  username: ""
  password: ""
  item_index_prefix: ms_search_item
  request_timeout: 5s
  max_response_bytes: 4194304
  debug_enabled: true

recommend_debug:
  max_candidate_limit: 1000
  debug_enabled: true
  online_base_url: http://127.0.0.1:18082
  request_timeout: 5s
```

说明：

- 管理员初始账号通过 SQL seed 写入数据库，不在配置文件中放明文密码。
- `sso.enabled=true` 时，dashboard 登录页显示“单点登录”按钮；本地账号密码登录仍保留。
- `sso.app_secret` 只允许保存在服务端配置中，不得下发到浏览器。
- dashboard 使用 Redis 同步应用授权；推荐 Debug 从授权投影读取 Secret 后调用 `ms-rec-online`。
- `recommend_debug.online_base_url` 指向推荐在线服务，默认匹配 `services-deploy/search-rec-ad` 的本地端口。

## 6. 数据模型

### 6.1 t_admin

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | BIGSERIAL | 管理员 ID |
| `name` | VARCHAR(32) | 登录名，唯一 |
| `nickname` | VARCHAR(64) | 昵称 |
| `password` | VARCHAR(255) | bcrypt 密码 hash |
| `disabled` | BOOLEAN | 是否禁用 |
| `create_time` | TIMESTAMPTZ | 创建时间 |
| `last_update_time` | TIMESTAMPTZ | 最后更新时间 |

规则：

- 响应中不返回 `password`。
- 首期不提供管理员增删改 UI；管理员通过初始化脚本创建。
- 禁用管理员可通过数据库运维处理，禁用后登录失败。

### 6.2 t_app

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | BIGINT | appid，从 `100001` 开始 |
| `name` | VARCHAR(128) | 应用名称 |
| `secret` | VARCHAR(255) | 应用密钥 |
| `remark` | TEXT | 备注 |
| `disabled` | BOOLEAN | 是否删除或禁用 |
| `create_time` | TIMESTAMPTZ | 创建时间 |
| `last_update_time` | TIMESTAMPTZ | 最后更新时间 |

规则：

- `id` 对应 Header `x-dwzauth-appid`（规范十进制字符串）。
- `secret` 对应 Header `x-dwzauth-secret`。
- 删除应用使用软删除：`disabled=true`。
- 删除应用后必须删除 Redis `app_auth_{appid}`。
- 首期不区分服务权限；一个应用默认可访问 datareceiver、reconline、searchonline。

### 6.3 t_admin_log

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | BIGSERIAL | 日志 ID |
| `admin_id` | BIGINT | 操作管理员 ID；登录失败未知时为 `0` |
| `cate` | VARCHAR(32) | 分类 |
| `type` | VARCHAR(32) | 类型 |
| `content` | JSONB | 脱敏后的日志内容 |
| `create_time` | TIMESTAMPTZ | 创建时间 |

日志类型：

| cate | type | 说明 |
| --- | --- | --- |
| `AUTH` | `LOGIN_SUCCESS` | 登录成功 |
| `AUTH` | `LOGIN_FAILED` | 登录失败 |
| `AUTH` | `LOGOUT` | 退出登录 |
| `APP` | `CREATE` | 新增应用 |
| `APP` | `UPDATE` | 修改应用 |
| `APP` | `DELETE` | 删除应用 |
| `ES_DEBUG` | `INDEX_VIEW` | 查看 ES 索引 |
| `ES_DEBUG` | `DOC_VIEW` | 查看 ES 文档 |
| `ES_DEBUG` | `QUERY` | ES 查询调试 |
| `REC_DEBUG` | `VIEW` | 推荐 debug |

日志不得记录 password、secret、token、ES 密码、Redis 密码。

## 7. Redis 授权投影

Redis key：

```text
app_auth_{appid}
```

类型：hash。

字段：

| 字段 | 说明 |
| --- | --- |
| `id` | appid |
| `secret` | 应用密钥 |
| `disabled` | `true` 或 `false` |
| `updated_at` | 最近更新时间，RFC3339 |

Redis key：

```text
app_auth_allappids
```

类型：string(JSON)。

值示例：

```json
[
  {
    "appid": 100001,
    "disabled": false
  }
]
```

同步规则：

- 创建应用：写数据库，成功后将所有有效应用重新刷到 Redis，并覆盖各自的 Redis hash 和 `app_auth_allappids`。
- 修改应用：写数据库，成功后将所有有效应用重新刷到 Redis，并覆盖各自的 Redis hash 和 `app_auth_allappids`。
- 删除应用：写数据库 `disabled=true`，成功后删除 Redis key，并刷新 `app_auth_allappids`。
- Redis 同步失败时，管理端操作返回失败；避免数据库成功但在线授权未更新。

dashboard 授权 API 校验规则：

1. 只从 JSON body 读取 `appid` 和 `secret`；`x-request-id` 只用于追踪，不参与授权材料。
2. 校验 appid 为规范十进制正整数，secret 非空。
3. 读取 dashboard 内部 Redis 授权投影 `app_auth_{appid}`。
4. appid 不存在则失败。
5. `disabled=true` 则失败。
6. secret 不一致则失败。
7. 授权投影读取错误则失败，不放行。

失败响应统一：

```json
{
  "status": 401,
  "message": "invalid app authorization",
  "data": null,
  "request_id": "0000000000000001"
}
```

### 7.1 有效应用 appid 列表 API

dashboard 提供一个无鉴权接口，用于返回当前有效应用 appid 列表。

```http
GET /api/v1/auth/app
```

处理流程：

1. 优先读取 Redis `app_auth_allappids`。
2. 缓存存在时直接返回。
3. 缓存不存在时查询数据库里的 `disabled=false` 应用。
4. 将查询结果写回 Redis `app_auth_allappids`。
5. 返回统一响应，`data` 为 appid 列表。

规则：

- 不要求管理端 JWT。
- 只返回有效应用，因此 `disabled` 固定为 `false`。
- Redis 读取或写入异常时返回失败，不降级为忽略错误。

### 7.2 应用授权校验 API

dashboard 增加一个对外授权校验接口，用于调用方以 HTTP 方式验证 `appid + secret` 是否有效。该接口只做校验，不创建会话，不返回应用资料，不替代应用管理接口。

```http
POST /api/v1/auth/app
Content-Type: application/json
```

请求体：

```json
{
  "appid": 100001,
  "secret": "app-secret"
}
```

成功响应：

```json
{
  "status": 200,
  "message": "success",
  "data": null,
  "request_id": "0000000000000001"
}
```

失败响应：

```json
{
  "status": 401,
  "message": "invalid app authorization",
  "data": null,
  "request_id": "0000000000000001"
}
```

处理流程：

1. 只接受 JSON body 中的 `appid` 和 `secret`，不从 Query 读取 secret。
2. 校验 `appid` 为正整数，`secret` 非空；字符串形式 appid 必须使用规范十进制正整数。
3. 读取 dashboard 内部 Redis 授权投影 `app_auth_{appid}`。
4. appid 不存在、`disabled=true`、secret 为空或 secret 不一致时返回失败。
5. 授权投影不可用或读取异常时返回失败，不降级读取数据库。
6. 调用方只根据 JSON `status` 判断结果：`status=200` 为成功，其他值为失败。
7. 成功和失败响应的 `data` 都返回 `null`，不返回 secret、应用名或备注。
8. appid 不存在、secret 不存在、`appid + secret` 不匹配等业务错误都返回失败，并统一使用 `invalid app authorization`。

日志要求：

- 不写 `t_admin_log`，因为该接口不是管理员操作。
- 访问日志可记录 `appid`、status、request_id、cost_ms。
- 任何日志不得记录 secret 明文。

## 8. 管理端登录

登录接口只做账号密码登录，不做 refresh token。

流程：

1. 接收 `name/password`。
2. 查询 `t_admin`。
3. 检查管理员存在且未禁用。
4. bcrypt 校验密码。
5. 写登录成功或失败日志。
6. 成功时签发 JWT access token。

JWT claims：

| Claim | 说明 |
| --- | --- |
| `admin_id` | 管理员 ID |
| `sub` | 管理员登录名 |
| `iss` | `ms-sar-dashboard` |
| `iat` | 签发时间 |
| `exp` | 过期时间 |

退出登录：

- 服务端返回成功。
- 前端清理本地 token。
- 服务端不保存 token，不做 token 黑名单。

## 9. 应用管理

应用管理接口提供：

- 创建应用。
- 分页查询应用。
- 修改应用。
- 删除应用。

查询规则：

- 默认只查 `disabled=false`。
- 支持按 `app_id` 精确查询。
- 支持按 `name` 模糊查询。
- 返回 `items/page/page_size/total`。

删除规则：

- 首期使用软删除。
- 删除后 Redis key 必须删除。
- 三个在线服务下一次授权必须失败。

## 10. ES Debug

ES Debug 是只读工具，不是 Kibana 替代品。首期只做能排障的最小集合。

索引规则：

```text
ms_search_item_{appid}_v1
```

功能：

- 按 appid 查看索引是否存在。
- 查看 mapping。
- 查看 settings。
- 查看文档数量。
- 按 `appid + item_id` 查看单文档。
- 对指定 appid 的索引执行只读 `_search`。
- 使用 Raw 输入执行只读 ES 请求，输入格式如 `GET /user/xxx` 后跟 JSON body，输出保留 ES 返回 JSON 结构。

禁止：

- ES 写操作。
- `_bulk`、`_delete_by_query`、`_update_by_query`、`_reindex`。
- Raw 模式使用 `POST`、`PUT`、`DELETE`、`PATCH` 等写方法。
- 无限制通配符查询。

审计：

- 每次查看索引、查看文档、执行查询都写 `t_admin_log`。
- 日志记录 appid、index、operation、request_id、cost_ms、success、error。
- 查询 DSL 只记录摘要或截断后的脱敏内容。

## 11. 推荐 Debug

推荐 Debug 是只读工具，不复制推荐引擎逻辑。页面按类型调用 `ms-rec-online` 的推荐接口，推荐排序、召回和兜底逻辑以线上推荐服务为准。

支持：

- 热门推荐：调用 `/api/v1/msrec/recommend/hot`，只有选择 `hot` 时才展示并传递 `hour`、`day`、`week`、`quarter`、`all`。
- 相关推荐：调用 `/api/v1/msrec/recommend/related`。
- 个性化推荐：调用 `/api/v1/msrec/recommend/personalized`。
- 展示推荐接口路径、请求参数、返回 item 列表和原始响应。

不做：

- 不提供直接输入 Redis key 的入口。
- 不写 Redis 或删除 Redis key。
- 不训练模型。

审计：

- 每次推荐 debug 写 `t_admin_log`。
- 日志记录 appid、debug_type、endpoint、result_count、request_id、cost_ms、success、error。
- 不记录完整候选列表到审计日志。

## 12. 管理端 UI

入口：

```text
/sar-admin
```

首期页面：

- 登录页。
- 授权应用列表。
- 新增应用。
- 修改应用。
- 删除应用。
- 审计日志列表。
- ES Debug。
- 推荐 Debug。

UI 规则：

- Secret 默认脱敏。
- 创建或修改 secret 后可以展示一次明文，便于复制。
- Debug 页面必须明确当前 appid。
- Debug 页面只读，不出现写入、删除、重建索引或修改 Redis 的按钮。

## 13. 安全约束

- 管理员密码必须 bcrypt hash。
- 生产环境 JWT secret 必须由环境注入。
- 应用 secret 不写日志。
- 管理端 token 不写数据库和 Redis。
- 三个在线服务不得继续使用固定 `x-dwz-auth` 或本地静态 token。
- 三个在线服务只通过 dashboard 授权接口授权。
- 应用授权校验 API 只从 JSON body 读取 secret，不允许 Query 传递 secret。
- dashboard 明确授权失败时不得放行；授权接口异常时在线服务只能按各自文档化的本地成功授权缓存策略处理。
- ES Debug 只允许只读操作。
- 推荐 Debug 只允许调用推荐查询接口，不提供 Redis key 直查、写入或删除能力。

## 14. 验收标准

- dashboard 可账号密码登录。
- 登录成功和失败都有审计日志。
- dashboard 可新增、分页查询、修改、删除应用。
- 应用创建、修改、删除后 Redis `app_auth_{appid}` 与数据库一致。
- `ms-data-receiver`、`ms-rec-online`、`ms-search-online` 都使用 `x-dwzauth-appid`、`x-dwzauth-secret`、`x-request-id`。
- 三个在线服务都只调用 dashboard 授权接口校验授权。
- 删除应用后，三个在线服务授权失败。
- `POST /api/v1/auth/app` 可通过 `appid + secret` 返回授权成功或失败。
- ES Debug 可查看索引、mapping、settings、count、单文档，并执行受控只读查询。
- 推荐 Debug 可调用 hot、related、personalized 三类推荐接口并展示返回结果。
- 管理端写操作和 debug 操作都有审计日志。
