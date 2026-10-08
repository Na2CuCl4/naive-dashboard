# MVP 技术设计与数据模型

2026-10-08，已确认范围沿用 development-handoff.md。本文件描述正在实现的单实例 MVP。

## 运行方式

Go 1.22.12 + SQLite WAL（单写连接、事务），嵌入原生 ES modules 前端。无需 Node 构建，也不增加前端服务。中央容器持久化 `/data`，专用 SSH 私钥只读挂载 `/run/secrets/ssh`。首次启动以 secret 文件提供管理员密码；cookie 会话、CSRF、登录速率限制。生产 Dashboard 应置于 HTTPS 反向代理后。

节点人工安装 root 管理的 naive-node；授权密钥强制执行固定 RPC，不允许任意 SSH 命令。中央使用 SSH 库，不读取宿主 SSH 配置，使用数据库中人工核对的 SHA256 主机指纹。调用 `naive-node rpc`，凭据只通过 stdin JSON。每次 RPC 有超时。中央后台每 60 秒采集、检查到期和额度、同步配置；页面也可以显式触发。

## 模型

| 表 | 核心字段与约束 |
| --- | --- |
| users | id、name、enabled、deleted、expires_at、quota_bytes（0=不限）、subscription_token；软删除保留统计与历史凭据映射 |
| nodes | id、SSH 地址/端口/用户/key_ref/host_fingerprint、代理地址/端口/protocol、desired_version、applied_version、health、last_seen、last_usage、error_code、cursor JSON |
| grants | user_id + node_id 主键、username、password、enabled；每用户每节点独立随机凭据，禁用/移除授权不会丢失历史映射 |
| identities | username 主键 → user_id、node_id；轮换保留旧用户名以结算旧连接 |
| sessions | node_id + epoch + session_id 主键、user_id、up、down、final、last_at；累计高水位 |
| usage | user_id + node_id + month 主键、up、down；按事件时间 UTC 月份记差额 |
| audit | 操作时间、操作类型、对象 ID；不保存密码、目标地址、订阅 token |

额度为累计总用量软额度，90% 提醒，达到额度则期望配置撤销权限；增加额度可恢复。到期同理。已有连接和离线节点允许延迟，UI 显示期望/已应用版本。月份用于报表，不自动重置累计额度。SQLite 与备份含代理凭据和订阅 token，限制文件权限并保护备份；MVP 不声称数据库字段加密。

## 配置与订阅

完整期望凭据集合排序后生成内容版本。节点对版本及内容校验幂等，拒绝旧版本/同版本异内容。仅替换管理员预留的 `# NAIVE_USERS`，校验完整候选 Caddyfile，原子安装、reload 并确认；失败恢复文件及运行配置。空用户集合生成不可使用的随机认证哨兵，禁止误变开放代理。操作串行并保留恢复状态；显式重启与普通同步分开。

用户授权同时控制节点认证集合和订阅 URI。订阅使用独立随机 token，代理凭据和订阅 token 分开轮换。URI 使用 `naive+https://` 或 `naive+quic://`，带 URL 编码用户信息；禁止缓存，禁用/到期/超额用户不再输出凭据。尚未应用的期望配置可能短暂出现客户端连接失败或待撤销，页面必须显示此事实。

## 一致计量

forwardproxy 保留现有 go.mod/go.sum 依赖，隔离开发 Caddy 2.8.4、quic-go 0.44.0，无额外 BBR 补丁。生产实测 Caddy 2.11.2 的完整 fork 源码提交未知，不能宣称测试构建可等同替换。生产升级与发布另行验收。

模块在目标连接层计数：上传为成功写入目标的 payload 字节，下载为从目标读取的 payload 字节，不含 Naive padding、TLS 或链路开销。每进程随机 epoch、每隧道唯一 session_id、代理用户名 user_id。每 60 秒以及连接双向结束时，向本机专用只追加 JSONL 文件 append + fsync 同样的累计事件 `{epoch,session_id,user_id,up,down,final,at}`。按路径共享进程级 tracker，reload 后旧连接继续结算。

节点工具只读导出完整行与每文件字节游标；短连接最终事件无需等待轮询即可持久化。中央在一个 SQLite 事务中更新每会话 `max(old,new)` 高水位、按方向差额更新用量、保存游标。重试/重复/乱序不重复入账；未知身份或损坏事件报错且不提交游标。旧进程文件保留可重放，重启 epoch 不冲突；读到部分尾行不前移。首期不自动轮转/清理：禁止外部 truncate/删除未备份确认的日志，空间容量需监控。崩溃可能丢失最后一次成功快照后的字节（通常最多约一分钟）；磁盘失败可扩大窗口并应报警，无法承诺崩溃零丢失。月份按快照时间归属增量，跨月一分钟内归属近似，不伪称精确账单。

## HTTP 与本地化

管理员 API `/api/session`、`/api/login`、`/api/logout`、`/api/state`、用户/节点资源及动作；错误始终为 `{error: stable_code}`。UI 只支持 zh-CN/en，使用完整同键翻译资源，语言偏好持久化。统计标明代理字节、最后采集时间和延迟。
