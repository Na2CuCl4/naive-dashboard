# 开发交接

更新时间：2026-10-08（Asia/Shanghai）。本文记录用户已确认的范围和此前调研结论，供新会话直接继续开发。项目目前是文档骨架，不是已完成的服务。

## 项目与仓库

- Dashboard：`/home/ubuntu/naive-dashboard`，origin 为 `git@github.com:Na2CuCl4/naive-dashboard.git`，当前工作分支 `codex/bootstrap`。
- 代理模块：`/home/ubuntu/forwardproxy`，origin 为 `git@github.com:Na2CuCl4/forwardproxy.git`，当前 `naive` 跟踪 `origin/naive`。
- 代理基线 HEAD：`c096d6a00cb28e019cc1995b04bdc6a9311d8024`，来自 klzgrad 的 `naive`。
- 代理 upstream：`https://github.com/klzgrad/forwardproxy.git`；caddy-upstream：`https://github.com/caddyserver/forwardproxy.git`。
- GitHub SSH 已成功认证为 `Na2CuCl4`。不要将 GitHub 登录密钥当作 VPN 节点的控制密钥。
- `/home/ubuntu/sub2api` 只是之前的远程工作目录，与本项目无关。

## 已确认的需求

1. 面向小规模、稳定的自用 Naive 集群；坚持原生 Naive。无需改成 Trojan/VLESS，也不以 Clash 普通 HTTPS 代理兼容为目标。
2. Dashboard 独立部署，必须提供 Docker/Compose 方案；可以部署在不承担代理流量的服务器。数据库、统计游标等需要持久化。
3. 所有节点均可由 Dashboard 通过 SSH 访问。首次人工 bootstrap；后续由面板管理节点和配置，不要求新的公网节点管理 API。
4. 用户创建/禁用/删除、用户—节点授权、凭据轮换、专属订阅链接、到期停用。
5. 节点登记、连通性与健康状态、期望/已应用配置版本、校验/热重载/失败恢复、显式服务重启。
6. 按用户和节点统计上传下载，连接结束结算；仍在传输的长连接每分钟刷新一次。用户已允许修改并重新编译 forwardproxy，为此维护小型 fork。
7. 软额度：接近额度提醒，超额后撤销节点上的代理权限；允许采集/同步延迟和已有连接超额。严格实时限额、限速及单用户立即踢线不属于首期承诺。
8. 第一版仅支持简体中文 `zh-CN` 和英文 `en`。使用翻译资源、语言切换和持久化偏好；页面、表单校验、状态和错误提示都覆盖两种语言。建议默认中文；API 使用稳定错误码，不将本地化文本作为业务状态。
9. 自主实现 Dashboard，不基于 NaivePanel 二开。可以使用成熟通用库，不必引入额外订阅转换平台。

## 固定版本与生产边界

用户明确要求本次不合并或 cherry-pick caddyserver/forwardproxy 的上游更新，也不为统计功能顺带升级依赖、Caddy 或 QUIC 实现。上游 remote 仅用于查看和比对。

此前只读核对本机 Caddy 为 v2.11.2，运行配置位于 `/etc/caddy/Caddyfile`，启用了 forward_proxy 和 probe_resistance，当时未启用 access log。新会话应重新核对实际状态，避免输出认证信息；未授权将新构建直接替换正在服务的 Caddy。

最新克隆的 Naive 分支构建流程使用 Caddy v2.11.7 和独立 QUIC BBRv1 补丁，与本机正在运行的二进制不同。fork 当前代码不代表已批准升级生产。先确认并记录可复现的开发基线，构建/功能验证使用隔离端口或测试容器。普通 xcaddy 模块替换不会自动应用 Caddy BBR 补丁，详见 `forwardproxy-fork.md`。

## SSH 与配置应用

实现固定的 `naive-node` 管理工具：支持状态、用户配置应用、用量读取和显式服务操作。调用使用固定命令和结构化 stdin；用户名、密码不拼接 shell，也不放进进程参数。

- 每节点专用控制密钥；私钥由 Dashboard 专属 secret 保存，数据库存密钥引用。
- 首次在节点手动安装工具、创建受限管理用户并授权公钥；在面板登记地址、端口、SSH 用户、密钥引用和核对过的主机指纹。
- 严格验证主机密钥；authorized_keys 限制强制命令，禁止转发、TTY、agent forwarding。节点端可执行文件及提权入口必须由可信管理员控制，不能让受限用户任意改写脚本后再 sudo。
- Docker 挂载项目自己的 secret/known_hosts，不依赖宿主机全局 ssh_config，不挂载整个宿主机 ~/.ssh。
- 版本化完整期望用户集合，幂等应用、串行锁；先校验完整候选配置，再原子安装、reload、确认结果。失败恢复磁盘和运行状态；只维护专用片段，保留节点域名、证书和其他站点配置。
- 离线节点显示待同步/待撤销，不把中央数据库更新等同于节点生效。
- 已有 CONNECT、复用 HTTP/2 下的新请求和 HTTP/3 的 reload/撤销行为均需验证。不要保证 reload 绝对不断线或必然立即踢掉禁用用户。

## 统计设计的关键要求

现有 Caddy 日志提供 `user_id`、`bytes_read`、`size`，但 CONNECT 通常在结束时才记录；每分钟读取日志不能获得活跃连接用量。需要模块内计数器和仅本机可访问的统计出口，节点工具通过受限 SSH 导出数据。

推荐以单调累计上传/下载计数作为快照和最终记录的共同语义：

- 身份键包含稳定的节点 ID、进程 epoch、隧道/session ID；reload 不能导致 ID 冲突或丢掉旧隧道计数。
- 数据库持久化每会话已入账计数，新快照按差额更新；最终记录只补剩余差额并完成结算。
- **不能简单相加“活跃快照 + 完整最终日志”**，否则重复计算同一批字节。
- 最终记录应可持久读取/重放，覆盖两次轮询之间结束的短连接。处理重复、乱序、日志轮转、读取中断、重试、重启和跨月连接。
- 模块计数和最终记录采用相同层次，明确是否包含 Naive padding。原生 access log 未必带同一 session ID/计量口径，需要显式关联或生成一致的最终事件，不能直接强行校正。
- 持久记录节点、用户、时间、字节等必要统计，认证头和私钥不得入日志；不以访问目标明细为产品功能。
- 呈现代理用量、最近上报时间和采集延迟，不把代理字节声称为云厂商账单或网卡计费。

## 授权与订阅

用户—节点授权必须同时体现在服务器认证与订阅内容。建议每用户每节点独立代理凭据；订阅 token 与代理凭据独立轮换。重置/删除订阅链接不会撤销已下载的代理密码。

原生输出 `naive+https://`/`naive+quic://` URI 列表，必要时提供 JSON 导出。已调研的 v2rayN 7.25.4 和 NekoBox Android 1.4.2 存在相关订阅解析；Shadowrocket 及用户实际客户端版本仍需端到端验证。不要保证所有版本或不同客户端使用完全相同的 Naive 网络栈。

## 技术选择和开发顺序

技术栈尚未最终确定。小规模单实例可优先评估 Go + SQLite、Vue/TypeScript（或其他轻量前端），前端从第一版加入 i18n；这是建议而非额外用户硬要求。无需为本项目引入 Kubernetes 或复杂多服务系统。

1. 读取 README 和 docs，检查两仓库工作区、当前工具链和构建流程。补最小技术设计、数据模型、节点协议及阶段验收，随后继续实现，不只停留在方案。
2. Docker/Compose 可运行骨架、管理员认证及双语 UI。
3. 节点 bootstrap/工具与 SSH 管理、用户授权和原生订阅。
4. forwardproxy 计数器、每分钟采集、幂等入账和软额度。
5. 在隔离测试节点验证配置失败恢复、鉴权撤销、长短连接计量和持久化恢复；记录仍需用户设备完成的客户端验收。

本机 GitHub SSH 可用不代表任何 VPN 节点已接入；节点凭据尚未交付给本项目。缺少真实节点时继续使用本地测试环境，关键产品歧义再询问用户。
