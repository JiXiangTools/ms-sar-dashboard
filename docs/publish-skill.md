# 测试环境发布流程

用于将 `ms-sar-dashboard` 发布到 Dell 测试环境。

## 1. 本地提交并推送远程

在本地仓库完成变更后，先测试、提交并推送到远程 `main`：

```bash
go test ./...
git status --short --branch
git add <changed-files>
git commit -m "<message>"
git pull --rebase origin main
git push origin main
```

## 2. Dell 更新代码并打包镜像

在 Dell 的项目目录更新最新代码，并执行镜像构建脚本：

```bash
ssh dell
cd /home/caturbhuja/8T/kely-workspace/ms-sar-dashboard
git status --short --branch
git pull --ff-only
bash admin/build-image.sh
```

记录 `admin/build-image.sh` 输出的最新镜像，例如：

```text
dockerhub.seobot.cc/ms/sar-dashboard:<tag>
```

## 3. 更新部署镜像并重启服务

进入测试环境部署目录，更新 `MS_SAR_DASHBOARD_IMAGE` 为上一步生成的新镜像，然后重启服务：

```bash
cd /home/caturbhuja/8T/kely-workspace/services-deploy/search-rec-ad
vi .env
docker compose up -d --force-recreate --no-deps ms-sar-dashboard
```

发布后验证服务状态和健康接口：

```bash
docker compose ps ms-sar-dashboard
curl --noproxy '*' -fsS http://127.0.0.1:8588/health
docker compose images ms-sar-dashboard
docker compose logs --tail=80 ms-sar-dashboard
```
