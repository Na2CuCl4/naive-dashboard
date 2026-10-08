# Naive Dashboard

小规模自用的原生 Naive 集群管理 MVP。单个 Go 服务、SQLite、简体中文/英文界面，通过受限 SSH 管理节点；代理流量不经过 Dashboard。提供用户与节点授权、独立凭据/订阅轮换、到期与累计软额度、节点配置校验/热重载/恢复、每分钟长连接用量与最终结算。

本项目与 sub2api 无关。当前测试基线和生产本机 Caddy 不同，运行中的生产 Caddy 未被替换。使用前请阅读 [构建基线](docs/build-baseline.md)、[技术设计](docs/technical-design.md) 和 [验证结果](docs/validation.md)。

## Docker/Compose 启动

需要 Docker Compose。生成独立管理员密码（16–72 字节），并给容器 UID 10001 读取权限：

```bash
mkdir -p secrets/ssh
umask 077
openssl rand -base64 32 > secrets/admin_password
sudo chown -R 10001:10001 secrets
sudo chmod 700 secrets secrets/ssh
sudo chmod 400 secrets/admin_password
docker compose up -d --build
```

容器默认绑定宿主 `127.0.0.1:8080`，由你自己的 HTTPS 反向代理访问，cookie 默认只允许 HTTPS。仅在本机 HTTP 开发时使用：

```bash
DASHBOARD_COOKIE_SECURE=false docker compose up -d --build
```

浏览器访问 `http://127.0.0.1:8080`。使用密码文件中的值登录，不存在默认密码。管理员密码从文件读取，修改文件并重建容器后生效；会话最多 12 小时，服务重启后需要重新登录。持久数据位于命名卷 `dashboard-data`，不要用 `docker compose down -v` 删除真实数据。

| 环境变量 | 默认值 |
| --- | --- |
| DASHBOARD_ADDR | :8080 |
| DASHBOARD_DB | /data/dashboard.db |
| DASHBOARD_ADMIN_PASSWORD_FILE | /run/secrets/admin_password |
| DASHBOARD_SSH_KEY_DIR | /run/secrets/ssh |
| DASHBOARD_COOKIE_SECURE | true |
| DASHBOARD_PORT（Compose 宿主端口） | 8080 |

## 节点接入

1. 按 [节点安装指南](deploy/node/README.md) 人工安装 `naive-node` 和经过验证的带计量 Caddy；准备完整模板、专用用户、强制命令与最小 sudo 授权。
2. 每节点生成独立 SSH 控制密钥，将私钥以 `0400` 放到 `secrets/ssh/<key_ref>`，容器 UID 10001 所有；从可信渠道核对节点 SSH SHA256 主机指纹。
3. 在面板登记节点地址、密钥文件名和指纹，点击同步。首次无用户时仍启用不可猜测的认证哨兵，不会变为开放代理。
4. 创建用户、勾选授权节点，再同步。给客户端导入该用户的专属订阅链接。原生格式为 `naive+https://` / `naive+quic://` 列表。

中央每 60 秒采集、检查到期/额度并同步。页面显示期望/已应用版本；离线节点保留待撤销。额度是累计上传+下载，0=不限，90% 提醒，达到额度后撤销后续认证；提高额度可以恢复。不会立即踢掉既有隧道。

代理密码与订阅 token 独立轮换。删除或轮换订阅链接不会撤销已下载密码，撤销密码需轮换代理凭据或禁用用户并等待节点应用。用户删除是保留历史统计的软删除；已有历史授权的节点首期保留记录，不提供破坏性删除。

## 本地开发与测试

固定 Go 1.22.12，Dashboard 编译需要 C 编译器（SQLite）。前端为原生 ES modules，无 npm 生产依赖。

```bash
go test -race ./...
go vet ./...
node --test web/i18n_test.mjs
./scripts/dev.sh
```

开发密码在 `.test/admin_password`。模块化代理构建在 sibling `../forwardproxy`，详见它的 `USAGE.md` 与 `scripts/build-isolated.sh`。本仓库提供独立 Docker SSH 测试节点，不需要真实节点凭据：

```bash
# 先生成 ../forwardproxy/build/naive-caddy
./scripts/integration/run.sh
./scripts/integration/cleanup.sh
```

测试固定项目名 `naive-mvp-test`、本机端口 18088/18089，使用一次性 SSH 密钥和专用数据库。流程包括短连接、真实一分钟快照、长连接跨 reload、认证轮换、去重、软额度、错误指纹拒绝和 Dashboard 容器重建持久化。脚本默认通过 `sudo -n docker` 调用 Docker。真实 Naive 客户端和生产版本互通状态见验证文档，不能将协议级测试当成所有客户端已验收。

## 数据与运维边界

- SQLite（含 WAL）与备份含代理凭据和订阅 token，文件和目录需受限；首期不做字段级加密。停服务后备份整个持久卷，或使用 SQLite backup API；不要只复制正在写的 `.db`。
- SSH 只使用项目自己的密钥目录，不使用 GitHub 密钥、不读取宿主 ssh_config、不挂载整个 ~/.ssh。指纹不匹配会拒绝连接。
- 节点计量目录必须是接管后的专用空目录。历史账号身份未导入会使统计拒绝该批次，以免错误归属。
- 用量是目标连接层的代理 payload，不含 Naive padding/TLS/网卡开销。重试和最终记录按累计差额入账；月份按事件 UTC 时间划分。
- 进程硬崩溃可能丢失最后成功快照后的字节；磁盘故障可扩大丢失窗口。首期不自动压缩/轮转日志，勿自行 truncate/删除；监控节点磁盘并备份。容量与日志确认清理是后续工作。
- 节点配置与 Caddy admin 只在节点本机操作。首次 bootstrap、可信模板调整和升级由管理员完成；面板没有远程任意命令入口。

原交接背景：[development-handoff.md](docs/development-handoff.md)。
