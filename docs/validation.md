# MVP 验证记录

日期：2026-10-08（Asia/Shanghai）。真实节点凭据未提供，所有节点验证均在本地隔离环境；不连接或修改生产 Caddy admin/config。

## 已通过

| 范围 | 验证与结果 |
| --- | --- |
| Dashboard/节点 Go | `GOMAXPROCS=2 GOFLAGS=-p=1 go test -race ./...`、`go vet ./...` 通过 |
| 累计入账 | 活跃快照→最终仅补差额，重复/乱序、短连接、重开数据库后重放、新epoch、跨UTC月份、未知身份事务回滚、游标拒绝回退、旧代理凭据最终结算 |
| 用户/认证 | 管理员cookie+CSRF、未登录拒绝、独立订阅/代理轮换、到期/禁用/软额度、离线保留待撤销；读API/订阅不被SSH等待锁住 |
| 节点工具 | fake Caddy 隔离测试：版本/内容幂等与冲突、零用户deny、完整候选校验、reload失败恢复、事务中断恢复、运行/磁盘漂移拒绝、完整行游标、半行/截断/缺文件/重复读取 |
| i18n | `node --test web/i18n_test.mjs` 4项通过：中英资源键与占位符一致、代码键覆盖、语言持久化、严格CSP兼容 |
| Chromium UI | 真实后端通过登录、用户/节点/授权、订阅与轮换、禁用/软删除、历史节点保护、zh-CN/en跨刷新、390px手机布局；无JS/CSP错误 |
| forwardproxy | 固定根module图全量race通过；真实TLS HTTP/2 padding回显证明 payload 计量与最终/快照一致，晚结束上传不会提前final |
| reload协议 | 独立 Caddy 子进程 HTTP/2、HTTP/3 测试通过：旧隧道继续传输/最终结算；旧复用连接新stream被拒绝；fresh连接旧密码407、新密码200；reload保持epoch |
| 构建复现 | Go1.22.12、Caddy2.8.4、原有quic-go0.44.0；两次固定构建二进制SHA256相同，根go.mod/go.sum无修改 |
| Docker全链路 | 实际SSH强制RPC、指纹错误拒绝、首次空授权、短连接、63秒真实分钟快照、旧隧道跨reload/旧凭据撤销、独立轮换、重复采集、额度撤销、Dashboard容器重建持久化通过；累计上传1525、下载1525字节，与发送payload严格一致 |

Docker测试节点的代理端口使用仅回环发布的HTTP/1 CONNECT回显，订阅解析只核对原生URI/凭据格式；TLS、Naive padding、HTTP/2和HTTP/3分别由fork的真实协议测试覆盖，不混同为实际客户端验收。

首次并行编译因测试机约2GB内存触发过OOM，降低GOMAXPROCS/Go并发后完成。此限制未通过修改服务或依赖规避。

## 如何复测

```bash
# Dashboard仓库
go test -race ./...
go vet ./...
node --test web/i18n_test.mjs

# fork（需要固定Go1.22.12）
cd ../forwardproxy
GOMAXPROCS=2 GOFLAGS=-p=1 go test -mod=readonly -race ./...
./scripts/build-isolated.sh
(cd cmd/naive-caddy && GOMAXPROCS=2 GOFLAGS=-p=1 go test -mod=readonly -v -run TestIsolatedReloadHTTP2AndHTTP3 .)

cd ../naive-dashboard
./scripts/integration/run.sh
./scripts/integration/cleanup.sh
```

浏览器复测脚本 `web/browser_test.mjs` 需要隔离安装 Playwright/Chromium，以及一个空的本地 Dashboard 实例；脚本头部有环境变量说明。不会把浏览器测试依赖添加到产品构建。

## 未验收与明确边界

- 尚未使用用户的真实节点、实际 Naive 客户端、v2rayN/NekoBox/Shadowrocket版本做端到端订阅与传输验收。
- 生产 Caddy2.11.2 的 fork源码提交无法从构建元数据还原。本次开发构建固定2.8.4仅用于隔离验证；生产部署不是本次完成项，不能直接替换生产二进制。
- 代理重启使用新epoch并保留原日志；硬崩溃只保证重放已fsync记录，最后成功快照之后字节可能丢失。正常final与snapshot完全去重，不把access log再次加总。
- 日志按进程文件只追加，支持旧文件重放与新文件发现；截断/缺文件会明确报错，不静默跳过。尚无自动压缩、确认后清理、磁盘容量报警服务。
- 软额度和到期撤销最终一致，既有隧道可能继续；后台一分钟采集加模块一分钟快照，采集与同步相位可能使显示延迟接近两分钟。页面分别显示最近成功采集、最新已入账事件及可能的批次积压。
- 配置写操作串行；离线节点可能延迟其他管理写操作。只读概览与订阅已通过独立SQLite事务避免该阻塞。
- 有历史授权的节点保留记录，首期只删除从未授权节点。用户软删除仍保留统计和旧身份映射。
- 订阅 token/代理密码在受限SQLite及备份中保存；字段级加密、MFA、多管理员和审计页面尚未提供。

生产核对：宿主Caddy进程仍为原PID1492977（2026-10-06启动），二进制SHA256仍为 `9a8a4d2cf9dd14040086cf5f1762eb8b4304f1dbc0c85784d8bdf27c2587956b`。没有执行宿主Caddy reload/restart、没有合并/cherry-pick上游更新。
