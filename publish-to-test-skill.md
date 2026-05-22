# 测试环境发布流程

本文记录 Dell 测试环境的搜广推服务发布流程。后续发布遵循本流程；如果某个项目没有新提交，不需要重新构建和发布该项目。

## 目标环境

- 机器：`dell`
- 工作目录：`/home/caturbhuja/8T/kely-workspace`
- 部署目录：`/home/caturbhuja/8T/kely-workspace/services-deploy/search-rec-ad`
- 相关项目：
  - `ms-sar-dashboard`
  - `ms-data-receiver`
  - `ms-search-online`
  - `ms-rec-online`

## 发布前检查

1. 登录 Dell 后确认各项目存在且没有未提交改动：

```bash
ssh dell 'for d in ms-sar-dashboard ms-data-receiver ms-search-online ms-rec-online services-deploy; do
  echo "==== $d"
  cd /home/caturbhuja/8T/kely-workspace/$d
  git status --short --branch
done'
```

2. 对比本地和远端提交。只有项目有新提交时才发布：

```bash
ssh dell 'for d in ms-sar-dashboard ms-data-receiver ms-search-online ms-rec-online; do
  echo "==== $d"
  cd /home/caturbhuja/8T/kely-workspace/$d
  git fetch origin main
  git status --short --branch
done'
```

3. 拉取需要发布的项目：

```bash
cd /home/caturbhuja/8T/kely-workspace/<project>
git pull --ff-only
git rev-parse --short HEAD
```

如果 `git pull --ff-only` 失败，先处理远端/本地分叉，不要强推或硬重置。

## 构建镜像

在每个需要发布的项目目录执行：

```bash
SYNC_COMPOSE_IMAGE=false admin/build-image.sh
```

记录脚本最后一行输出的镜像名，例如：

```text
dockerhub.seobot.cc/ms/sar-dashboard:260522_e7d2b8a
dockerhub.seobot.cc/ms/data-receiver:260522_1d8436e
dockerhub.seobot.cc/ms/search-online:260522_bc006d3
dockerhub.seobot.cc/ms/rec-online:260522_5eb5bb0
```

注意：`ms-search-online` 如果构建时因为 `golang:1.22-alpine` 拉取失败，可以临时复制 Dockerfile 并替换基础镜像为 Dell 本机已有镜像后构建，不提交该临时文件：

```bash
cd /home/caturbhuja/8T/kely-workspace/ms-search-online
sed \
  -e 's#^FROM golang:1.22-alpine AS builder#FROM dockerhub.seobot.cc/library/golang:1.26 AS builder#' \
  -e 's#^FROM alpine:3.20#FROM dockerhub.seobot.cc/library/alpine:3.20#' \
  Dockerfile > Dockerfile.codex-build
DOCKERFILE_PATH=Dockerfile.codex-build admin/build-image.sh
rm -f Dockerfile.codex-build
```

## 替换部署镜像

进入部署目录：

```bash
cd /home/caturbhuja/8T/kely-workspace/services-deploy/search-rec-ad
```

修改 `.env` 中对应镜像变量，只替换本次有更新的项目：

```text
MS_DATA_RECEIVER_IMAGE=<new image>
MS_SEARCH_ONLINE_IMAGE=<new image>
MS_REC_ONLINE_IMAGE=<new image>
MS_SAR_DASHBOARD_IMAGE=<new image>
```

确保三个在线服务带有 dashboard 授权配置：

```text
MS_SAR_DASHBOARD_AUTH_URL=http://ms-sar-dashboard:8081/api/v1/auth/app
MS_SAR_DASHBOARD_AUTH_CACHE_TTL=10m
MS_SAR_DASHBOARD_AUTH_TIMEOUT=3s
```

如果 `docker-compose.override.yml` 还没有注入上述变量，需要在 `ms-data-receiver`、`ms-search-online`、`ms-rec-online` 的 `environment` 下添加：

```yaml
MS_SAR_DASHBOARD_AUTH_URL: ${MS_SAR_DASHBOARD_AUTH_URL}
MS_SAR_DASHBOARD_AUTH_CACHE_TTL: ${MS_SAR_DASHBOARD_AUTH_CACHE_TTL}
MS_SAR_DASHBOARD_AUTH_TIMEOUT: ${MS_SAR_DASHBOARD_AUTH_TIMEOUT}
```

## 启动服务

只重建本次更新的服务。示例：

```bash
docker compose up -d --force-recreate --no-deps \
  ms-sar-dashboard \
  ms-data-receiver \
  ms-search-online \
  ms-rec-online
```

如果只有部分项目更新，只写对应服务名。

## 验证

1. 查看容器状态：

```bash
docker compose ps --format 'table {{.Service}}\t{{.State}}\t{{.Status}}\t{{.Ports}}'
```

目标服务应为 `running` 且 `healthy`：

- `ms-sar-dashboard`，端口 `8588`
- `ms-data-receiver`，端口 `18080`
- `ms-search-online`，端口 `18081`
- `ms-rec-online`，端口 `18082`

2. 检查健康接口：

```bash
curl --noproxy '*' -fsS http://127.0.0.1:8588/health
curl --noproxy '*' -fsS http://127.0.0.1:18080/health
curl --noproxy '*' -fsS http://127.0.0.1:18081/health
curl --noproxy '*' -fsS http://127.0.0.1:18082/health
```

3. 确认运行镜像：

```bash
docker compose images ms-sar-dashboard ms-data-receiver ms-search-online ms-rec-online
```

4. 检查三个在线服务的授权环境变量：

```bash
docker compose exec -T ms-data-receiver printenv | grep MS_SAR_DASHBOARD_AUTH
docker compose exec -T ms-search-online printenv | grep MS_SAR_DASHBOARD_AUTH
docker compose exec -T ms-rec-online printenv | grep MS_SAR_DASHBOARD_AUTH
```

5. 查看关键日志：

```bash
for svc in ms-sar-dashboard ms-data-receiver ms-search-online ms-rec-online; do
  echo "==== $svc"
  docker compose logs --tail=80 "$svc" | grep -Ei 'error|panic|fatal|auth|dashboard|started|listening|failed' || true
done
```

## 提交部署配置

如果修改了 `services-deploy/search-rec-ad/docker-compose.override.yml` 等版本化部署配置，需要提交并推送：

```bash
cd /home/caturbhuja/8T/kely-workspace/services-deploy
git status --short
git add search-rec-ad/docker-compose.override.yml
git commit -m "chore: wire dashboard auth for search rec ad"
git pull --rebase origin main
git push origin main
```

`.env` 通常是环境文件，不提交；只在 Dell 测试环境保留当前镜像变量。
