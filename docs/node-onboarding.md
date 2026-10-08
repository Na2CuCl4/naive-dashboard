# 节点接入方案

Dashboard 使用 Docker 部署时，不需要修改宿主机的全局 `/etc/ssh/ssh_config`。每个节点的地址、端口、SSH 用户、密钥标识和主机指纹保存在 Dashboard 数据库；SSH 私钥和 `known_hosts` 放在项目自己的受保护目录或 Docker Secret 中。

## 首次接入

首次接入建议由管理员在目标 VPN 节点上手动完成一次 bootstrap：

1. 安装固定的 `naive-node` 管理程序。
2. 创建专用的节点管理用户，或为现有运维用户配置最小化 `sudo` 权限。
3. 将 Dashboard 的公钥加入 `authorized_keys`，并限制为 `naive-node` 强制命令。
4. 禁用该密钥的 TTY、端口转发、X11 转发和 Agent forwarding。
5. 在节点外部核对 SSH 主机指纹。
6. 在 Dashboard 中录入地址、端口、SSH 用户和已核对的主机指纹。
7. Dashboard 通过受限 SSH 执行 `naive-node status`，成功后才将节点标记为可管理。

首次 bootstrap 不由网页端执行任意安装脚本，也不自动接受未知主机指纹。这样可以避免把一个输入框变成远程 root 执行入口，也避免 `StrictHostKeyChecking=no` 带来的中间人风险。

## 后续管理

节点登记后，Dashboard 可以自动完成：

- 分发版本化用户清单；
- 生成 Caddy 用户片段；
- `caddy validate`、原子替换和 `caddy reload`；
- 查询节点状态和配置版本；
- 读取本地统计采集器的增量；
- 显式执行重启、回滚和密钥轮换。

Dashboard 只调用固定的 `naive-node` 子命令，不直接执行任意 shell。节点端脚本负责校验输入、维护备份和控制 Caddy 服务。

## Docker 中的 SSH 文件

容器只挂载 Dashboard 自己的 SSH 私钥和 `known_hosts`：

```text
/run/secrets/dashboard_ssh_key   # 只读，权限 0400
/data/ssh/known_hosts            # 只读或由 Dashboard 管理
/data/dashboard.db               # 数据库和同步状态
```

不挂载宿主机整个 `/root/.ssh`，也不修改宿主机的全局 SSH 配置。节点较少时，每个节点使用独立的控制密钥；节点删除或密钥轮换时，只需要撤销对应节点上的公钥。

## 为什么不立即自动添加节点

Dashboard 可以提供“添加节点”页面，但页面首期只登记连接信息并测试 SSH。真正的首次安装、密钥授权、Caddy 配置准备和主机指纹核对由管理员手动完成。等节点协议稳定后，再增加使用一次性 bootstrap token 的半自动安装流程。

