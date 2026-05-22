# ms-sar-dashboard 管理端 API 文档

## 1. 公共约定

管理端 API 前缀：

```text
/api/v1/admin
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
| `GET` | `/api/v1/admin/app` | 是 | 应用分页列表 |
| `POST` | `/api/v1/admin/app` | 是 | 创建应用 |
| `PUT` | `/api/v1/admin/app/{app_id}` | 是 | 修改应用 |
| `DELETE` | `/api/v1/admin/app/{app_id}` | 是 | 删除应用 |
| `GET` | `/api/v1/admin/log` | 是 | 审计日志 |
| `GET` | `/api/v1/admin/debug/es/index/{appid}` | 是 | ES 索引信息 |
| `GET` | `/api/v1/admin/debug/es/doc/{appid}/{item_id}` | 是 | ES 文档查看 |
| `POST` | `/api/v1/admin/debug/es/search/{appid}` | 是 | ES 只读查询 |
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
- 同步 Redis `app_auth_{appid}`。
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
- 覆盖 Redis `app_auth_{appid}`。
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
| `x-dwzauth-appid` | 是 | 应用 ID |
| `x-dwzauth-secret` | 是 | 应用密钥 |
| `x-request-id` | 否 | 请求追踪 ID |

校验：

- 读取 Redis `app_auth_{appid}`。
- key 不存在，失败。
- `disabled=true`，失败。
- secret 不匹配，失败。
- Redis 不可用，失败。

失败响应：

```json
{
  "status": 401,
  "message": "invalid app authorization",
  "data": null,
  "request_id": "0000000000000001"
}
```

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

按索引 `ms_search_product_{appid}_v1` 和 `_id={item_id}` 查看文档。

### 查询调试

```http
POST /api/v1/admin/debug/es/search/{appid}
```

请求体为 ES `_search` Query DSL。

限制：

- 只允许访问 `ms_search_product_{appid}_v1`。
- 只允许 `_search`。
- 限制响应体大小和超时时间。
- 禁止 ES 写操作。

## 11. 推荐 Debug

```http
POST /api/v1/admin/debug/rec
```

请求：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `type` | string | 是 | `hot`、`related`、`personalized` |
| `appid` | string | 是 | 应用 ID |
| `item_id` | string | 否 | 相关推荐使用 |
| `user_id` | string | 否 | 个性化使用 |
| `period` | string | 否 | 仅热门使用：`hour`、`day`、`week`，默认 `day` |
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
