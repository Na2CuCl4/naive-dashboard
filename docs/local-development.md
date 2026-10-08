# 换机器继续开发

两个仓库的开发成果已归并到各自 `main`。源码、Go module 锁文件、前端资源、Dockerfile、测试和节点示例均已提交；不依赖原服务器的绝对路径。首次下载用 clone，已有 checkout 用 pull。

## 推荐环境

完整流程已在 Linux amd64 验证。Windows 使用 WSL2 + Linux 工具链；macOS 建议使用 Linux 虚拟机做完整节点集成验证。Dashboard/节点包含 Unix syscall，Windows 原生编译不受支持。macOS 原生面板开发未验收，现有集成脚本在宿主编译 Linux 容器内使用的二进制，不能直接当作 macOS Docker Desktop 的一键流程。

准备 Git、Go **1.22.12**、C 编译器（gcc，SQLite CGO 所需）、Bash、OpenSSL。跑全部测试还需 Node.js 24、Python3（节点测试使用 `/usr/bin/python3`）。SSH/Docker 集成需要 OpenSSH client、Docker Engine、Compose，以及脚本所用的 `sudo -n docker`/文件属主权限。原生 Naive 客户端验收另行准备。

前端无需 npm install 或打包。可选 Chromium UI 测试需要单独安装 Playwright，见 [web/README.md](../web/README.md)。Go 与 Docker 首次运行需要联网下载已锁依赖和镜像。

## 下载和启动面板

在本地选一个父目录，将两仓库并排下载：

```bash
git clone --branch main git@github.com:Na2CuCl4/naive-dashboard.git
git clone --branch main git@github.com:Na2CuCl4/forwardproxy.git
cd naive-dashboard
go version  # 应为 go1.22.12
go mod download
./scripts/dev.sh
```

浏览器访问 `http://127.0.0.1:8080`，登录密码在 `.test/admin_password`。脚本自动创建本地 SQLite、密码和空 SSH 目录，不需要真实节点就可以开发页面/API。

前端文件嵌入 Go 二进制；改完 Go 或前端后重启 `scripts/dev.sh` 才能看到变化，首期没有热更新服务。

已有 checkout 的日常更新（先处理自己的未提交修改）：

```bash
git switch main
git pull --ff-only origin main
```

两仓库分别执行。已有 forwardproxy 的旧 `naive` 本地分支，可先按 GitHub 的改名步骤改为 `main`、fetch 并设置跟踪 `origin/main`。若本地 main 尚不存在，使用 `git fetch origin` 后 `git switch --track origin/main`。

## 测试与本地节点

```bash
# 在 naive-dashboard
go test -race ./...
go vet ./...
node --test web/i18n_test.mjs

# 原有依赖基线的计量 Caddy，仅作隔离开发
cd ../forwardproxy
go mod download
go test -mod=readonly -race ./...
./scripts/build-isolated.sh

# 独立 Docker SSH 节点；无需真实 VPN 节点密钥
cd ../naive-dashboard
./scripts/integration/run.sh
./scripts/integration/cleanup.sh
```

测试占用回环端口 18088/18089，使用固定项目名 `naive-mvp-test` 和一次性密钥。不要并行启动两份。较小机器可设置 `GOMAXPROCS=2` 和 `GOFLAGS=-p=1` 降低编译并发。仅开发中央面板时，无需先编译 forwardproxy。

开发基线仍是 Caddy2.8.4/Go1.22.12/quic-go0.44.0，不使用上游 Caddy2.11.7/BBR 发布工作流。不要将开发二进制直接替换正在服务的 Caddy2.11.2；完整版本边界见 [build-baseline.md](build-baseline.md)。

## 不随 Git 携带的内容

- `secrets/`、`.env`、SSH 私钥、公钥授权与运行节点指纹配置。
- `.test/`、SQLite 数据库、Compose 数据卷、统计游标和管理员密码。
- `forwardproxy/build/`、Docker 本地镜像、编译缓存、浏览器截图。

新机器从空开发数据启动即可。若要继续管理真实节点，需要另行安全迁移凭据和数据库备份，而不是只执行 pull。不要把这些文件提交到 Git。原服务器的 GitHub SSH 身份也不会自动复制到新机器；需使用你本机已授权的 GitHub SSH key。
