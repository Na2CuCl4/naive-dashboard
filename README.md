# Naive Dashboard

独立的 NaiveProxy/Caddy 集群管理 Dashboard（规划中）。

本项目与 `/home/ubuntu/sub2api` 无关，默认以 Docker Compose 部署。Dashboard 通过受限 SSH 管理各节点上的固定 `naive-node` 管理程序；代理流量不经过 Dashboard。

第一版仅支持简体中文（`zh-CN`）和英文（`en`），从第一版建立翻译资源及语言切换。

## 当前状态

项目目录已建立，目前只有规划文档，尚无 Dashboard 或节点程序实现。项目尚未接入 VPN 节点，也未保存节点管理私钥或节点指纹。GitHub SSH 认证已验证；它与 VPN 节点的管理凭据是两回事。节点接入应先在节点上手动完成安全 bootstrap，再由 Dashboard 管理后续配置。

开发从 [docs/development-handoff.md](docs/development-handoff.md) 开始；新会话可使用 [docs/start-prompt.md](docs/start-prompt.md) 中的提示词。

节点接入和 Docker 中的 SSH 文件布局见 [docs/node-onboarding.md](docs/node-onboarding.md)。

Naive 代理模块的 fork 选择、提交关系和构建注意事项见 [docs/forwardproxy-fork.md](docs/forwardproxy-fork.md)。
