# Naive Dashboard

独立的 NaiveProxy/Caddy 集群管理 Dashboard（规划中）。

本项目与 `/home/ubuntu/sub2api` 无关，默认以 Docker Compose 部署。Dashboard 通过受限 SSH 管理各节点上的固定 `naive-node` 管理程序；代理流量不经过 Dashboard。

## 当前状态

项目目录已建立，尚未连接任何 VPN 节点，也没有保存 SSH 私钥或主机指纹。节点接入应先在节点上手动完成安全 bootstrap，再由 Dashboard 管理后续配置。

节点接入和 Docker 中的 SSH 文件布局见 [docs/node-onboarding.md](docs/node-onboarding.md)。

Naive 代理模块的 fork 选择、提交关系和构建注意事项见 [docs/forwardproxy-fork.md](docs/forwardproxy-fork.md)。
