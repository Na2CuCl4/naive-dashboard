# 新会话启动提示词

先按 [本地开发指南](local-development.md) clone 两个仓库，再在 Dashboard 目录开始新会话。可发送以下提示词，无需沿用原服务器路径：

```text
请继续开发 Naive Dashboard。当前目录是 naive-dashboard；计量代理模块在同级 ../forwardproxy。两个仓库的开发成果已归并到 main，先检查工作区和最新提交。

先读 README.md、docs/local-development.md、docs/development-handoff.md、docs/technical-design.md、docs/build-baseline.md、docs/validation.md 和 ../forwardproxy/USAGE.md，继承已确认需求，不重新讨论选型。MVP 已实现，不要从文档骨架重新开始。

现有功能：Go/SQLite、Docker/Compose、zh-CN/en UI、受限SSH节点管理、用户/节点授权、独立凭据与订阅轮换、配置校验/reload/失败恢复、累计快照与最终结算、软额度。测试覆盖race、浏览器、Docker完整链路及HTTP2/HTTP3 reload。真实节点和用户实际Naive客户端仍待验收。

forwardproxy 原基线 c096d6a，计量提交5d81539已进入main。根go.mod/go.sum保持原依赖；隔离开发Caddy2.8.4/Go1.22.12/quic-go0.44.0，不合并/cherry-pick caddyserver更新，不捆绑升级Caddy、依赖或QUIC。原服务器生产Caddy2.11.2不可直接替换为开发构建。

GitHub SSH与节点控制密钥分开。缺少真实节点凭据时使用项目本地隔离测试节点；不接触原服务器生产服务，也不操作sub2api。普通实现选择自行判断，关键歧义再确认。

请先概述当前代码和验收缺口，再按照我接下来的具体需求继续实现。
```
