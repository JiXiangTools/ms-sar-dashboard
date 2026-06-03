# ms-sar-dashboard 管理端 API 文档

## 1. 公共约定

管理端 API 前缀：

```text
/api/v1/admin
```

应用授权校验 API：

```text
/api/v1/auth/app
```

后台页面入口：

```text
/sar-admin
```

管理端鉴权：

```text
Authorization: Bearer <access_token>
```

统一响应：

```json
{
  "status": 200,
  "message": "success",
  "data": {},
  "request_id": "0000000000000001"
}
```

## 2. 接口列表

| 方法 | 路径 | 鉴权 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/health` | 否 | 健康检查 |
| `POST` | `/api/v1/admin/auth/login` | 否 | 登录 |
| `POST` | `/api/v1/admin/auth/logout` | 是 | 退出登录 |
| `POST` | `/api/v1/auth/app` | 否 | 应用授权校验 |
| `GET` | `/api/v1/admin/app` | 是 | 应用分页列表 |
| `POST` | `/api/v1/admin/app` | 是 | 创建应用 |
| `PUT` | `/api/v1/admin/app/{app_id}` | 是 | 修改应用 |
| `DELETE` | `/api/v1/admin/app/{app_id}` | 是 | 删除应用 |
| `GET` | `/api/v1/admin/log` | 是 | 审计日志 |
| `GET` | `/api/v1/admin/debug/es/index/{appid}` | 是 | ES 索引信息 |
| `GET` | `/api/v1/admin/debug/es/doc/{appid}/{item_id}` | 是 | ES 文档查看 |
| `POST` | `/api/v1/admin/debug/es/search/{appid}` | 是 | ES 只读查询 |
| `POST` | `/api/v1/admin/debug/es/raw` | 是 | ES Raw 只读调试 |
| `POST` | `/api/v1/admin/debug/rec` | 是 | 推荐 debug |

首期不提供 refresh token，不提供管理员管理 API。

## 3. 登录

```http
POST /api/v1/admin/auth/login
```

请求：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | string | 是 | 管理员账号 |
| `password` | string | 是 | 管理员密码 |

响应：

```json
{
  "status": 200,
  "message": "success",
  "data": {
    "access_token": "<access_token>",
    "token_type": "Bearer",
    "expires_in": 7200
  },
  "request_id": "0000000000000001"
}
```

规则：

- 登录成功写 `t_admin_log`：`cate=AUTH`，`type=LOGIN_SUCCESS`。
- 登录失败写 `t_admin_log`：`cate=AUTH`，`type=LOGIN_FAILED`。
- 日志不得记录密码和 token。
- 登录失败对外统一返回 `invalid admin credentials`。

## 4. 应用列表

```http
GET /api/v1/admin/app
```

Query：

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `app_id` | integer | 否 | 应用 ID |
| `name` | string | 否 | 应用名模糊搜索 |
| `page` | integer | 否 | 默认 `1` |
| `page_size` | integer | 否 | 默认 `10`，最大 `100` |

响应：

```json
{
  "status": 200,
  "message": "success",
  "data": {
    "items": [
      {
        "id": 100001,
        "name": "mall-app",
        "secret": "secret-1",
        "remark": "商城应用",
        "disabled": false,
        "create_time": "2026-05-19T09:00:00+08:00",
        "last_update_time": "2026-05-19T09:00:00+08:00"
      }
    ],
    "page": 1,
    "page_size": 10,
    "total": 1
  },
  "request_id": "0000000000000001"
}
```

说明：

- `id` 对应 `x-dwzauth-appid`。
- `secret` 对应 `x-dwzauth-secret`。
- 默认只返回 `disabled=false` 的应用。
- UI 默认脱敏展示 secret。

## 5. 创建应用

```http
POST /api/v1/admin/app
```

请求：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | string | 是 | 应用名称 |
| `secret` | string | 否 | 不传则服务端生成 |
| `remark` | string | 否 | 备注 |

规则：

- 写入 `t_app`。
- 将所有有效应用重新刷到 Redis，覆盖各自的 `app_auth_{appid}`。
- 写审计日志：`cate=APP`，`type=CREATE`。
- Redis 同步失败时创建失败。

## 6. 修改应用

```http
PUT /api/v1/admin/app/{app_id}
```

请求：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | string | 否 | 应用名称 |
| `secret` | string | 否 | 新密钥 |
| `remark` | string | 否 | 备注 |

规则：

- 至少传一个字段。
- 写入 `t_app`。
- 将所有有效应用重新刷到 Redis，覆盖各自的 `app_auth_{appid}`。
- 写审计日志：`cate=APP`，`type=UPDATE`。
- Redis 同步失败时修改失败。

## 7. 删除应用

```http
DELETE /api/v1/admin/app/{app_id}
```

规则：

- 软删除：`t_app.disabled=true`。
- 删除 Redis `app_auth_{appid}`。
- 写审计日志：`cate=APP`，`type=DELETE`。
- Redis 删除失败时删除失败。

## 8. 在线服务统一授权

`ms-data-receiver`、`ms-rec-online`、`ms-search-online` 统一使用：

| Header | 必填 | 说明 |
| --- | --- | --- |
| `x-dwzauth-appid` | 是 | 应用 ID，必须为规范十进制正整数 |
| `x-dwzauth-secret` | 是 | 应用密钥 |
| `x-request-id` | 否 | 请求追踪 ID |

校验：

- 在线服务调用 `POST /api/v1/auth/app`。
- appid 不存在，失败。
- 应用已禁用，失败。
- secret 不存在或不匹配，失败。
- dashboard 授权 API 自身不可用或超时时失败；在线服务如需临时沿用本地成功授权缓存，必须以各自服务文档为准。

失败响应：

```json
{
  "status": 401,
  "message": "invalid app authorization",
  "data": null,
  "request_id": "0000000000000001"
}
```

### 8.1 应用授权校验 API

该接口用于调用方主动校验 `appid + secret` 是否有效。接口本身不要求管理端 JWT，鉴权材料只允许从 JSON body 读取，不从 Query 读取 secret。

```http
POST /api/v1/auth/app
Content-Type: application/json
```

请求：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `appid` | integer | 是 | 应用 ID，必须为规范十进制正整数 |
| `secret` | string | 是 | 应用密钥，前后空格会被裁剪 |

示例：

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

规则：

- 调用方只根据 JSON `status` 判断结果：`status=200` 为成功，其他值为失败。
- 校验逻辑读取 dashboard 内部应用授权投影 `app_auth_{appid}`。
- `appid` 为空、非正整数，或 `secret` 为空时返回失败；字符串形式 appid 必须使用规范十进制正整数。
- appid 不存在、应用已禁用、secret 不匹配时返回失败。
- 授权投影不可用或读取异常时返回失败，不允许降级为通过。
- 失败场景包含 appid 不存在、secret 不存在、`appid + secret` 不匹配等业务错误；对外统一返回 `invalid app authorization`。
- 不写 `t_admin_log`，只走访问日志和错误日志；日志中不得记录 secret 明文。
- 该接口只做授权校验，不返回应用 secret、名称、备注等管理信息。

## 9. 审计日志

```http
GET /api/v1/admin/log
```

Query：

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `cate` | string | 否 | `AUTH`、`APP`、`ES_DEBUG`、`REC_DEBUG` |
| `type` | string | 否 | 日志类型 |
| `page` | integer | 否 | 默认 `1` |
| `page_size` | integer | 否 | 默认 `10`，最大 `100` |

日志内容必须脱敏。

## 10. ES Debug

ES Debug 只允许只读操作。

### 索引信息

```http
GET /api/v1/admin/debug/es/index/{appid}
```

返回索引是否存在、mapping、settings、count、health 等只读信息。

### 文档查看

```http
GET /api/v1/admin/debug/es/doc/{appid}/{item_id}
```

按索引 `ms_search_item_{appid}_v1` 和 `_id={item_id}` 查看文档。

### 查询调试

```http
POST /api/v1/admin/debug/es/search/{appid}
```

请求体为 ES `_search` Query DSL。

限制：

- 只允许访问 `ms_search_item_{appid}_v1`。
- 只允许 `_search`。
- 限制响应体大小和超时时间。
- 禁止 ES 写操作。

### Raw 只读调试

```http
POST /api/v1/admin/debug/es/raw
```

请求：

```json
{
  "input": "GET /user/xxx\n\n{}"
}
```

也可拆分传入：

```json
{
  "method": "GET",
  "path": "/user/xxx",
  "body": "{}"
}
```

规则：

- 首行格式为 `GET /path` 或 `HEAD /path`。
- 后续内容必须为空或合法 JSON。
- 输出 `data` 为 ES 返回的 JSON 原文结构。
- 禁止 `POST`、`PUT`、`DELETE`、`PATCH` 等写方法。
- 禁止绝对 URL、`_bulk`、`_delete_by_query`、`_update_by_query`、`_reindex` 等写入或敏感入口。

## 11. 推荐 Debug

```http
POST /api/v1/admin/debug/rec
```

请求：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `type` | string | 是 | `hot`、`related`、`personalized` |
| `appid` | string | 是 | 应用 ID，必须为规范十进制正整数 |
| `item_id` | string | 否 | 相关推荐使用 |
| `user_id` | string | 否 | 个性化使用 |
| `period` | string | 否 | 仅热门使用：`hour`、`day`、`week`、`quarter`、`all`，默认 `day` |
| `size` | integer | 否 | 默认 `20` |
| `exclude` | array | 否 | 排除 item_id |

返回：

- 实际调用的推荐接口路径。
- 透传给推荐接口的参数。
- 推荐接口返回的 `item_ids` 和 `size`。
- 推荐接口原始响应。

限制：

- 从 dashboard Redis 授权投影读取应用 Secret，再调用 `ms-rec-online` 推荐接口。
- 不提供直接输入 Redis key 的调试入口。
- 不写候选结果。
- 推荐排序、召回和兜底逻辑以 `ms-rec-online` 当前接口为准。
