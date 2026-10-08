# 新会话启动提示词

在 `/home/ubuntu/naive-dashboard` 开始新会话，发送以下内容：

```text
请接手并开始实现 Naive Dashboard。先阅读 README.md、docs/development-handoff.md、docs/node-onboarding.md、docs/forwardproxy-fork.md，再检查两个仓库状态，继承文档中的已确认决定。

Dashboard 独立位于 /home/ubuntu/naive-dashboard；Naive 代理模块 fork 位于 /home/ubuntu/forwardproxy。两者 origin 都在 GitHub Na2CuCl4 账号下，GitHub SSH 已验证。/home/ubuntu/sub2api 与本项目无关。

目标是小规模自用的原生 Naive 集群管理：Docker/Compose 部署中央 Dashboard，通过受限 SSH 调用节点工具，管理用户、用户—节点授权、凭据轮换、Naive 订阅、配置校验/热重载/回滚和服务健康。节点首次人工 bootstrap，后续面板管理。GitHub SSH 与节点管理凭据分开；真实节点缺失时使用隔离测试节点继续开发。

第一版仅支持简体中文 zh-CN 和英文 en，从第一版建立 i18n 资源与语言切换。我们自己实现 Dashboard，不基于 NaivePanel 二开，也不转向 Trojan/VLESS 或 Clash 普通 HTTPS 兼容路线。

我已允许修改/重编译 forwardproxy，以实现连接结束结算 + 活跃长连接每分钟快照，以及软额度。快照和最终记录须统一口径，使用会话累计计数按差额入账，防止重复计费并覆盖短连接、重试和重启。无需承诺严格实时配额或立即踢线。

本次明确不合并/cherry-pick caddyserver 上游更新，不捆绑升级依赖、Caddy 或 QUIC 实现。先核对并固定可复现基线；不要直接替换或重启本机正在服务的 Caddy。各仓库使用 codex/ 功能分支，保留已有修改。

请先补最小技术设计和验收清单，然后持续实现可运行 MVP，完成适当测试及 Docker 验证。普通实现选择自行判断，有影响范围的关键问题再向我确认。
```
