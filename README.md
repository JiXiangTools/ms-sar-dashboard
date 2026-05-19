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
- [docs/agent.md](./docs/agent.md)：协作约束和后续实现注意事项

## 参考项目

- `../ms-data-receiver`：现有数据接入服务，当前包含配置式管理员登录和本地 `data/apps.json` 应用授权配置。
- `../ms-rec-online`：在线推荐服务，后续改为使用 dashboard 统一应用授权。
- `../ms-search-online`：在线搜索服务，后续改为使用 dashboard 统一应用授权。
- `../ms-user-center`：目标分层参考，包含 PostgreSQL、Redis、JWT、管理员登录、应用授权、审计日志、后台 UI 和统一响应模式。

## 迁移与切换

目标状态：

- dashboard 管理员登录依赖 PostgreSQL，不再从配置文件读取管理员账号密码。
- dashboard 管理应用授权，并同步 Redis `app_auth_{appid}`。
- `ms-data-receiver`、`ms-rec-online`、`ms-search-online` 统一使用 `x-dwzauth-appid`、`x-dwzauth-secret`、`x-request-id`。
- 三个在线服务只从 Redis 校验授权，旧 `data/apps.json`、固定 `x-dwz-auth`、固定 `app.appid` 不再作为线上授权来源。

服务改造：

| 服务 | 当前授权 | 目标授权 |
| --- | --- | --- |
| `ms-data-receiver` | `data/apps.json` + `x-dwzauth-appid/secret` | Redis `app_auth_{appid}` + 统一 Header |
| `ms-rec-online` | 固定 `x-dwz-auth`，appid 来自配置 | Redis `app_auth_{appid}` + 统一 Header |
| `ms-search-online` | 固定 `x-dwz-auth` / allowed appids，请求体 appid | Redis `app_auth_{appid}` + 统一 Header |

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

1. 部署 dashboard 数据库和 Redis 配置。
2. 初始化管理员。
3. 迁移 `ms-data-receiver/data/apps.json`，校验 Redis `app_auth_{appid}`。
4. 切换 `ms-data-receiver`，保持商品/行为上报 API 和 Kafka 消息格式不变。
5. 切换 `ms-rec-online`，appid 改为来自 `x-dwzauth-appid`，推荐 Redis key 使用 Header appid。
6. 切换 `ms-search-online`，ES 索引使用 Header appid 路由；如短期保留请求体 appid，必须与 Header appid 一致。
7. 验证上报、推荐、搜索和 debug 页面。
8. 下线旧管理端、旧本地授权文件和旧固定 token。

回滚要求：

- 切换前备份 `data/apps.json`。
- 保留三个在线服务旧版本镜像。
- 回滚期间禁止同时在旧 data-receiver 后台和 dashboard 修改应用。
- 如果 Redis 授权异常，回滚在线服务到旧授权版本。

验收清单：

- dashboard：登录成功/失败、登录日志、应用创建/列表/修改/删除、Redis 同步、审计日志脱敏。
- data-receiver：统一 Header 授权成功，secret 错误、应用删除、Redis 不可用时失败，Kafka 消息格式不变。
- rec-online：统一 Header 授权成功，推荐 Redis key 使用 Header appid，推荐响应格式不变。
- search-online：统一 Header 授权成功，ES 索引使用 Header appid，搜索响应格式不变。
- debug：ES Debug 和推荐 Debug 只读，操作写审计日志。

## 本地开发

初始化 PostgreSQL 测试库和默认管理员：

```bash
RESET_DATABASE=true ./admin/init-local-pg.sh
```

默认管理员：

- 账号：`admin`
- 密码：`dWz@240926!`

如果本机 PostgreSQL DSN 不同，可以通过环境变量覆盖：

```bash
MSSAR_DATABASE_DSN='postgres://postgres:postgres@127.0.0.1:5432/ms_sar_dashboard_test?sslmode=disable' \
RESET_DATABASE=true \
./admin/init-local-pg.sh
```

启动本地调试服务：

```bash
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

## 测试

```bash
go test ./...
```

接口冒烟示例：

```bash
BASE_URL=http://127.0.0.1:8081 ./test/shell/curl.sh
```

## 容器与打包

- `Dockerfile`：运行镜像构建
- `docker-compose.yml`：本地 compose 编排，包含服务、PostgreSQL 和 Redis
- `admin/build-image.sh`：镜像打包
- `admin/docker-postgres-init.sh`：compose 内 PostgreSQL 初始化脚本

构建镜像：

```bash
./admin/build-image.sh
```

脚本默认生成 `dockerhub.seobot.cc/ms/sar-dashboard:{date}_{commit}`，并在构建成功后同步更新 `docker-compose.yml` 中的服务镜像。

启动本地 compose：

```bash
docker compose -f docker-compose.yml up -d
```

compose 默认暴露：

- 服务：`http://127.0.0.1:8588`
- 管理页面：`http://127.0.0.1:8588/sar-admin`
- PostgreSQL 数据库：`ms_sar_dashboard`
- Redis DB：`8`

## Offline 部署

`deploy/` 下提供从 `alg-rec-offline/test/docker-compose.yml` 提炼出的离线任务 compose，只保留 offline 服务，不包含 Kafka、Redis、Elasticsearch、data-receiver、search-online 和 rec-online 测试容器。

- `deploy/docker-compose.yml`：offline 任务编排
- `deploy/.env`：镜像、平台、Kafka/Redis/ES 地址、appid、topic 和调度参数

启动：

```bash
cd deploy
docker compose up -d
```

也可以从仓库根目录启动：

```bash
docker compose --env-file deploy/.env -f deploy/docker-compose.yml up -d
```

启动前需要按实际环境修改 `deploy/.env` 中的 `AROF_KAFKA_BOOTSTRAP_SERVERS`、`AROF_REDIS_URL`、`AROF_ES_URL` 和 `AROF_APPID`。

## 目录结构

- `cmd/ms-sar-dashboard`：服务启动入口
- `cmd/hash-password`：bcrypt 密码哈希生成工具
- `admin/`：本地初始化、调试启动和镜像构建脚本
- `configs/`：环境配置
- `deploy/`：离线推荐任务部署 compose 和环境变量
- `docs/`：设计、接口和协作约束文档
- `internal/app`：应用装配与生命周期
- `internal/http`：路由、handler、middleware、UI
- `internal/service`：核心业务逻辑
- `internal/repository`：数据库访问
- `upgrade/sql/`：表结构与初始化 SQL
- `test/`：契约、配置和冒烟测试
