# ms-sar-dashboard

`ms-sar-dashboard` 是搜广推后台管理服务，使用 Go 实现。它承接原来 `ms-data-receiver` 中的管理端登录、应用授权配置管理和后台页面能力，并参考 `ms-user-center` 的数据库、JWT、中间件、仓储、服务和后台 UI 分层。

目标边界：

- 登录、管理员账号、应用授权配置全部归属本项目，并通过数据库持久化。
- 授权应用统一供 `ms-data-receiver`、`ms-rec-online`、`ms-search-online` 使用。
- 三个在线服务统一使用请求头 `x-dwzauth-appid`、`x-dwzauth-secret`、`x-request-id`，并调用 dashboard `POST /api/v1/auth/app` 校验 `appid + secret`。
- `ms-data-receiver` 不再保存管理员账号、登录配置或应用配置文件，只在客户端上报时使用授权信息。
- 客户端上报链路仍由 `ms-data-receiver` 负责：校验授权、校验商品/行为数据、写 Kafka。
- 后台管理链路由 `ms-sar-dashboard` 负责：登录、token、管理员、应用授权配置、审计和后续搜广推运营管理页面。
- 可选对接 `ms-user-center` 管理端 CAS 单点登录；单点登录成功后仍由 dashboard 签发本地 JWT。

## 首期功能

- 账号密码登录，登录成功和失败都必须记录日志。
- 可选单点登录。
- 授权应用管理：新增、删除、修改、分页列表。

## 文档导航

- [docs/design.md](./docs/design.md)：总体设计、职责边界、数据模型和认证授权方案
- [docs/api-admin.md](./docs/api-admin.md)：管理端 API 契约
- [docs/agent.md](./docs/agent.md)：协作约束和后续实现注意事项

## 参考项目

- `../ms-data-receiver`：现有数据接入服务，当前包含配置式管理员登录和本地 `data/apps.json` 应用授权配置。
- `../ms-rec-online`：在线推荐服务，后续改为使用 dashboard 统一应用授权。
- `../ms-search-online`：在线搜索服务，后续改为使用 dashboard 统一应用授权。
- `../ms-user-center`：目标分层参考，包含 PostgreSQL、Redis、JWT、管理员登录、应用授权、审计日志、后台 UI 和统一响应模式。

## 迁移与切换

目标状态：

- dashboard 管理员登录依赖 PostgreSQL，不再从配置文件读取管理员账号密码。
- dashboard 管理应用授权，同步内部 Redis 授权投影，并提供 `POST /api/v1/auth/app` 校验接口。
- `ms-data-receiver`、`ms-rec-online`、`ms-search-online` 统一使用 `x-dwzauth-appid`、`x-dwzauth-secret`、`x-request-id`。
- 三个在线服务只调用 dashboard 授权接口，旧 `data/apps.json`、固定 `x-dwz-auth`、固定 `app.appid` 不再作为线上授权来源。

服务改造：

| 服务 | 当前授权 | 目标授权 |
| --- | --- | --- |
| `ms-data-receiver` | `data/apps.json` + `x-dwzauth-appid/secret` | dashboard 授权 API + 统一 Header |
| `ms-rec-online` | 固定 `x-dwz-auth`，appid 来自配置 | dashboard 授权 API + 统一 Header |
| `ms-search-online` | 固定 `x-dwz-auth` / allowed appids，请求体 appid | dashboard 授权 API + 统一 Header |

历史 `apps.json` 迁移到 `t_app` 时保留原 `appid` 和 `secret`，并在导入后调整 `t_app_id_seq` 到历史最大 appid 之后：

| apps.json | t_app |
| --- | --- |
| `appid` | `id` |
| `name` | `name` |
| `secret` | `secret` |
| `disabled` | `disabled` |
| `remark` | `remark` |
| `update_time` | `last_update_time` |

切换顺序：

1. 使用 `ms-sar-dbtool` 执行数据库迁移并初始化管理员。
2. 部署 dashboard 和 Redis 配置；应用启动时只校验迁移版本，不执行 DDL。
3. 迁移 `ms-data-receiver/data/apps.json`，校验 dashboard `POST /api/v1/auth/app`。
4. 切换 `ms-data-receiver`，保持商品/行为上报 API 和 Kafka 消息格式不变。
5. 切换 `ms-rec-online`，appid 改为来自 `x-dwzauth-appid`，推荐 Redis key 使用 Header appid。
6. 切换 `ms-search-online`，ES 索引使用 Header appid 路由；如短期保留请求体 appid，必须与 Header appid 一致。
7. 验证上报、推荐、搜索和 debug 页面。
8. 下线旧管理端、旧本地授权文件和旧固定 token。

回滚要求：

- 切换前备份 `data/apps.json`。
- 保留三个在线服务旧版本镜像。
- 回滚期间禁止同时在旧 data-receiver 后台和 dashboard 修改应用。
- 如果 dashboard 授权接口异常，回滚在线服务到旧授权版本。

验收清单：

- dashboard：登录成功/失败、登录日志、应用创建/列表/修改/删除、Redis 同步、审计日志脱敏。
- data-receiver：统一 Header 授权成功，secret 错误、应用删除时失败；dashboard 授权接口不可用且无历史成功缓存时失败，有历史成功缓存时按接入服务契约短期沿用；Kafka 消息格式不变。
- rec-online：统一 Header 授权成功，推荐 Redis key 使用 Header appid，推荐响应格式不变。
- search-online：统一 Header 授权成功，ES 索引使用 Header appid，搜索响应格式不变。
- debug：ES Debug 和推荐 Debug 只读，操作写审计日志。

## 本地开发

初始化 `services-deploy/base` 里的 PostgreSQL 数据库和管理员。脚本会幂等执行版本化迁移，不会删除已有表或数据；账号密码必须显式提供，仓库不包含默认管理员凭据：

```bash
MSSAR_INIT_ADMIN_NAME='your-admin' \
MSSAR_INIT_ADMIN_PASSWORD='replace-with-a-strong-password' \
./admin/init-local-pg.sh
```

应用默认为空，首次需要在 `sar-admin` 里手动新增。新增或修改应用时，Secret 会生成一串随机密文，并在提交后只展示一次，便于复制保存。

脚本需要通过 `MSSAR_SERVICES_DEPLOY_DIR` 显式指定本地 `services-deploy` 目录；如果你想改成别的 PostgreSQL 实例，也可以通过环境变量覆盖：

```bash
MSSAR_SERVICES_DEPLOY_DIR='../services-deploy' \
MSSAR_DATABASE_DSN='postgres://postgres:postgres@127.0.0.1:5432/ms_sar_dashboard?sslmode=disable' \
MSSAR_INIT_ADMIN_NAME='your-admin' \
MSSAR_INIT_ADMIN_PASSWORD='replace-with-a-strong-password' \
./admin/init-local-pg.sh
```

已有同名管理员默认保持原密码和禁用状态，避免部署时意外重置凭据。受控轮换时显式增加 `MSSAR_UPDATE_ADMIN=true`；需要清理本地授权缓存时使用 `RESET_AUTH_CACHE=true ./admin/start-debug.sh`。`RESET_DATABASE` 已禁用，破坏性测试请创建单独的一次性数据库。

启动本地调试服务会先确保 `services-deploy/base` 里的 PostgreSQL / Redis / Elasticsearch 就绪，再初始化缺失数据，不会预置默认应用。如果 `services-deploy/base/.env` 不存在，脚本会使用本地默认值启动中间件；Redis 默认密码为 `redis`，会自动注入到 dashboard 配置里。

```bash
MSSAR_INIT_ADMIN_NAME='your-admin' \
MSSAR_INIT_ADMIN_PASSWORD='replace-with-a-strong-password' \
./admin/start-debug.sh
```

指定配置：

```bash
CONFIG_PATH=./configs/test.yaml ./admin/start-debug.sh
```

后台页面入口：

```text
http://127.0.0.1:8081/sar-admin
```

如果要启用 `ms-user-center` 单点登录，需要补充以下配置：

```yaml
sso:
  enabled: true
  admin_ui_url: https://uc.example.com/uc-admin
  api_base_url: https://uc.example.com
  app_id: "100001"
  app_secret: "secret-from-user-center"
  redirect_url: https://sar.example.com/sar-admin
  request_timeout: 3s
```

说明：

- `app_id` / `app_secret` 对应 `ms-user-center` 里的 CAS 应用配置。
- `redirect_url` 需要回到 dashboard 登录页，例如 `/sar-admin`。
- 前端不会持有 `app_secret`；跳转 URL 和 CAS 换票都由 dashboard 服务端完成。
- 单点登录成功后会按用户中心 `account` 同步到本地 `t_admin.name`，后续鉴权统一以 `t_admin` 为事实源。

## 测试

```bash
go test ./...
```

设置专用测试 PostgreSQL 后，会额外运行真实迁移并发、旧库数据保留和 checksum 漂移测试：

```bash
MSSAR_MIGRATION_TEST_DSN='postgres://postgres:postgres@127.0.0.1:5432/ms_sar_dashboard_migration_test?sslmode=disable' \
go test -count=1 ./internal/platform/database
```

接口冒烟示例：

```bash
BASE_URL=http://127.0.0.1:8081 \
ADMIN_NAME='your-admin' \
ADMIN_PASSWORD='your-admin-password' \
./test/shell/curl.sh
```

## HTTP 安全边界

所有 HTTP 请求在进入路由 handler 前都会执行统一请求体限制，默认上限为 `1048576` 字节（1 MiB）。无论请求是否携带 `Content-Length`、是否使用 chunked 传输，超限都返回统一 JSON `413 request body too large`。可通过 `MSSAR_SECURITY_MAX_REQUEST_BODY_BYTES` 覆盖；该值必须为正数。

客户端 IP 默认只取 TCP socket peer，并进行 IPv4/IPv6 canonical 化；未经信任的 peer 提供的 `X-Forwarded-For` 一律忽略。只有部署在反向代理后时，才应通过逗号分隔的 `MSSAR_SECURITY_TRUSTED_PROXY_CIDRS` 配置实际代理网段，例如 `10.20.0.0/24,2001:db8:10::/64`。配置应尽可能收窄，不能把任意客户端可能直连的网段加入信任列表。解析时从 socket peer 沿 `X-Forwarded-For` 右侧向左回溯，限流和脱敏访问日志使用同一个 canonical client IP。

本仓与 `ms-data-receiver`、`ms-search-online`、`ms-rec-online` 联合生产部署时，该变量是上线必配项：必须精确包含这三类 online 实例/Pod 的来源网段。否则 Dashboard 会忽略 online 转发的终端 IP，多个终端会共享 online 实例的 app-auth 限流桶；部署检查应据此阻塞发布。

安全限流使用 Redis Lua 原子共享固定窗口，因此多个 dashboard 实例共享同一计数。Redis 调用默认最多等待 `100ms`，可通过 `MSSAR_SECURITY_RATE_LIMIT_STORE_TIMEOUT` 调整。每个实例同时维护有界的本机影子窗口；Redis 出错或超时时按本机窗口继续限流，不返回依赖故障 `5xx`，也不会无限放行。降级期间额度按实例计算，因此应同时告警 Redis 故障并尽快恢复共享计数。

密码登录使用两层额度：每个 canonical 来源 IP `100/min`，每个规范化账号 SHA-256 身份 `10/min`。账号、密码和应用 secret 均不会进入 Redis key、错误详情或日志；账号维度避免同一 NAT 下所有管理员共享原来的小额度。

## Prometheus 指标

服务公开无需鉴权的 `GET /metrics`，返回 Prometheus/OpenMetrics 文本。该入口用于集群内抓取，生产环境应通过网络策略或反向代理限制到监控系统，不应直接暴露到公网。

当前指标包括：

- `ms_sar_dashboard_http_requests_total` 和 `ms_sar_dashboard_http_request_duration_seconds`：仅使用 Gin 固定路由模板、HTTP method 和状态码标签。
- `ms_sar_dashboard_client_auth_total`：应用授权结果。
- `ms_sar_dashboard_admin_auth_total`：登录、SSO 登录和 access token 校验结果。
- `ms_sar_dashboard_authorization_projection_reconcile_total`：启动及管理员手动授权投影 reconcile 结果。
- `ms_sar_dashboard_startup_checks_total`：数据库 migration verify 结果；启动失败时进程不会提供可抓取端点，应结合启动日志和容器重启告警排查。

指标禁止使用 appid、admin id、request id、原始 URL path、query 或任何认证材料作为 label。未匹配路由统一标为 `unmatched`。

data-receiver 测试数据上报脚本：

```bash
DATA_RECEIVER_BASE_URL=http://127.0.0.1:8080 \
DWZ_APPID=100001 \
DWZ_APP_SECRET=secret-1 \
python3 ./test/test-items-upload.py

DATA_RECEIVER_BASE_URL=http://127.0.0.1:8080 \
DWZ_APPID=100001 \
DWZ_APP_SECRET=secret-1 \
python3 ./test/test-action-upload.py
```

- `test/test-items-upload.py` 调用 `POST /api/v1/msdr/item/report`，批量写入覆盖核心字段的商品/内容样例。
- `test/test-action-upload.py` 调用 `POST /api/v1/msdr/behavior/report`，写入覆盖全部当前 `event_type`、设备上下文、用户/session/匿名身份和行为扩展属性的用户行为样例。
- 运行前需确保 `ms-sar-dashboard` 已创建对应 `DWZ_APPID` / `DWZ_APP_SECRET`，并且 `POST /api/v1/auth/app` 可以校验通过。
- 如果 `ms-data-receiver` 客户端上报 API 的路径、Header、字段、枚举或批量限制发生修改，必须同步更新这两个脚本，避免联调和验收数据与真实接口契约脱节。

## 容器与打包

- `Dockerfile`：运行镜像构建
- `docker-compose.yml`：本地 compose 编排，包含一次性迁移任务、服务、PostgreSQL 和 Redis
- `admin/build-image.sh`：镜像打包
- `admin/docker-postgres-init.sh`：仅供旧的 PostgreSQL initdb/测试环境兼容引导；正式升级使用 `ms-sar-dbtool`

构建镜像：

```bash
./admin/build-image.sh
```

脚本默认生成 `dockerhub.seobot.cc/ms/sar-dashboard:{date}_{commit}`，并把同一镜像名原子写入被 Git 忽略的 `.compose-image.env`；不会修改 tracked `docker-compose.yml`。Dashboard 与 migration job 通过同一 Compose anchor 读取该变量，避免镜像漂移。

启动本地 compose：

```bash
export POSTGRES_PASSWORD='replace-with-a-strong-database-password'
export MSSAR_DATABASE_DSN='postgres://postgres:URL_ENCODED_PASSWORD@postgres:5432/ms_sar_dashboard?sslmode=disable'
export MSSAR_DATABASE_MIGRATION_DSN="${MSSAR_DATABASE_DSN}"
export MSSAR_AUTH_JWT_SECRET='replace-with-at-least-32-random-characters'
export MSSAR_SECURITY_MAX_REQUEST_BODY_BYTES='1048576'
# 仅部署在受控反向代理后时设置，例如：
# export MSSAR_SECURITY_TRUSTED_PROXY_CIDRS='10.20.0.0/24'
export MSSAR_INIT_ADMIN_NAME='your-admin'
export MSSAR_INIT_ADMIN_PASSWORD_HASH="$(go run ./cmd/hash-password -password 'replace-with-a-strong-admin-password')"
docker compose --env-file .compose-image.env -f docker-compose.yml up -d
```

以上变量均为必填，且执行 Compose 前必须先构建镜像或显式设置 `MS_SAR_DASHBOARD_IMAGE`；默认占位镜像 `ms-sar-dashboard:local` 不会静默回退到旧发布版本。示例为方便本地运行让两个 DSN 使用同一账号；生产环境建议给 `MSSAR_DATABASE_MIGRATION_DSN` 配置短生命周期 DDL 账号，给 `MSSAR_DATABASE_DSN` 配置仅有业务 DML、序列使用和迁移表只读权限的运行账号。Secret 应由受控管理系统或不入库的 `.env` 文件注入；JWT Secret 轮换会使现有管理端会话失效。`database-migrate` 会先取得 PostgreSQL advisory lock，按版本执行迁移并初始化缺失的管理员；只有它成功退出后 dashboard 才启动。已有同名管理员不会在每次部署时被覆盖。

compose 默认暴露：

- 服务：`http://127.0.0.1:8588`
- 管理页面：`http://127.0.0.1:8588/sar-admin`
- PostgreSQL 数据库：`ms_sar_dashboard`
- Redis DB：`8`

## 数据库迁移

迁移文件位于 `upgrade/sql/migrations/`，命名为六位递增版本号加名称，例如 `000001_initial.sql`。已发布的文件不可修改；任何结构变化都新增更高版本。迁移器把版本、名称、SHA-256 checksum 和执行时间写入 `schema_migrations`。

迁移保证：

- 使用固定 PostgreSQL advisory lock 串行化多个同时启动的迁移任务。
- 每个迁移 SQL 与对应版本记录在同一事务提交，失败时不留下半完成版本。
- 已执行版本会校验名称和 checksum；数据库含未知新版本、缺失中间版本或文件被修改时立即失败。
- 初始迁移可接管旧版 `schema.sql` 创建的同结构数据库，使用 `IF NOT EXISTS` 并保留已有管理员、应用、审计记录和 appid 序列。
- dashboard 运行进程只取得共享锁并校验版本，不自行执行 DDL；迁移账号和运行账号可以按最小权限拆分。

非 Compose 部署应在发布应用前显式执行：

```bash
MSSAR_DATABASE_DSN='postgres://migration-user:password@postgres:5432/ms_sar_dashboard?sslmode=verify-full' \
/app/ms-sar-dbtool -timeout 60s
```

需要首次初始化管理员时同时设置 `MSSAR_INIT_ADMIN_NAME` 和 `MSSAR_INIT_ADMIN_PASSWORD_HASH`。已有账号默认保持不变；只有人工确认的密码轮换才使用 `-update-admin`。迁移采用只前进策略：失败版本修正后重新执行，已发布版本有问题时新增修复版本，不通过回滚 SQL 删除已有数据。

## 目录结构

- `cmd/ms-sar-dashboard`：服务启动入口
- `cmd/ms-sar-dbtool`：显式数据库迁移和初始管理员工具
- `cmd/hash-password`：bcrypt 密码哈希生成工具
- `admin/`：本地初始化、调试启动和镜像构建脚本
- `configs/`：环境配置
- `docs/`：设计、接口和协作约束文档
- `internal/app`：应用装配与生命周期
- `internal/http`：路由、handler、middleware、UI
- `internal/service`：核心业务逻辑
- `internal/repository`：数据库访问
- `upgrade/sql/migrations/`：不可变的版本化数据库迁移
- `upgrade/sql/schema.sql`：psql 本地/E2E 兼容引导入口
- `test/`：契约、配置和冒烟测试
