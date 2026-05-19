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

启动本地 compose：

```bash
docker compose -f docker-compose.yml up -d
```

compose 默认暴露：

- 服务：`http://127.0.0.1:8588`
- 管理页面：`http://127.0.0.1:8588/sar-admin`
- PostgreSQL 数据库：`ms_sar_dashboard`
- Redis DB：`8`

## 目录结构

- `cmd/ms-sar-dashboard`：服务启动入口
- `cmd/hash-password`：bcrypt 密码哈希生成工具
- `admin/`：本地初始化、调试启动和镜像构建脚本
- `configs/`：环境配置
- `docs/`：设计、接口和迁移文档
- `internal/app`：应用装配与生命周期
- `internal/http`：路由、handler、middleware、UI
- `internal/service`：核心业务逻辑
- `internal/repository`：数据库访问
- `upgrade/sql/`：表结构与初始化 SQL
- `test/`：契约、配置和冒烟测试
