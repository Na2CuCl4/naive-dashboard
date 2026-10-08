# 可复现开发基线

2026-10-08 只读核查生产：`/usr/bin/caddy version` = v2.11.2，Go1.25.9，quic-go0.59.0，二进制 SHA256 `9a8a4d2cf9dd14040086cf5f1762eb8b4304f1dbc0c85784d8bdf27c2587956b`。构建信息中的 forwardproxy 为本地替换，不含可信源码提交，无法从现有二进制还原完整构建锁。未读取/记录生产认证信息，未替换二进制或 reload/restart 生产服务。

此次功能开发基线：

| 项目 | 固定值 |
| --- | --- |
| forwardproxy 原始 HEAD | c096d6a00cb28e019cc1995b04bdc6a9311d8024 |
| 本次计量实现 | codex/usage-metering，5d81539（本地提交） |
| 测试二进制 SHA256 | 61e8c09f82b9ee7b8e5331b784dd4578f6693b12fb5e5ba242592f1187cbfd47 |
| 根 go.mod / go.sum | 保持原样，不合并/cherry-pick 上游 |
| 开发 Caddy | v2.8.4（fork 根已锁版本） |
| 开发 QUIC | quic-go v0.44.0，无额外 BBR 补丁 |
| Go | 1.22.12 linux/amd64 |
| Go tarball SHA256 | 4fa4f869b0f7fc6bb1eb2660e74657fbf04cdd290b5aef905585c86051b34d43 |
| Dashboard SQLite driver | github.com/mattn/go-sqlite3 v1.14.22 |
| SSH | golang.org/x/crypto v0.23.0 |
| 构建镜像 | golang:1.22.12-bookworm@sha256:3d699e4d15d0f8f13c9195c0632a16702b8cbdece2955af1c23b37ae5d55a253 |
| 运行镜像 | debian:bookworm-slim@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587 |

forwardproxy 的 `cmd/naive-caddy` 使用独立 go.mod/go.sum 收录 standard 模块所需的完整依赖图；已有依赖版本未升级。构建方式见 fork 中 `scripts/build-isolated.sh` / `USAGE.md`。Go 构建使用 readonly module lock、trimpath。上游 workflow 的 Caddy2.11.7 / Go1.26 / xcaddy latest / BBR 补丁不参与本次构建。

此基线可用于隔离开发和功能回归；Caddy2.8.4 较老，不是向生产回退版本的建议，也不是可直接替代生产2.11.2的发布物。生产发布前仍需建立与节点现状一致的完整构建锁、Naive 客户端验证及升级安排。

Dockerfile 固定基础镜像 digest；系统 ca-certificates 等 apt 包来自 Debian 仓库，镜像层不是逐字节确定性发布物。正式发布应进一步锁 apt snapshot、产出 SBOM/签名和镜像 digest。此次不因此修改已有代理依赖。
