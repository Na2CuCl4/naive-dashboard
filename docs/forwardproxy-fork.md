# forwardproxy fork 调研与维护方案

核查日期：2026-10-08（Asia/Shanghai）。本次只调查远端历史并在临时目录试合并，未改变正在运行的 Caddy，也未建立 `/home/ubuntu/forwardproxy`。

## 结论

从 https://github.com/klzgrad/forwardproxy fork，使用其默认 `naive` 分支。不要以该仓库的旧 `master` 分支为开发起点，也不要直接用不含 Naive padding 的 `caddyserver/forwardproxy/master` 替换现有服务。

独立 fork 用于实现每用户的活动隧道计数、每分钟用量快照和最终结算。Dashboard、SSH 节点工具留在 naive-dashboard；数据转发层修改留在 forwardproxy fork。

## 当前提交关系

| 分支 | 提交 |
| --- | --- |
| caddyserver/forwardproxy `master` | `0aab84dad4fc2830789f34e27b4d7bc22a40889e` |
| klzgrad/forwardproxy `naive` | `c096d6a00cb28e019cc1995b04bdc6a9311d8024` |
| 共同祖先 | `b9def71846e5992b314c96a6fe66a05485ac66a5` |

相对 `caddyserver/master`，`klzgrad/naive` 有 3 个独有提交，同时缺少 Caddy 上游的 6 个提交；不是线性地落后 6 个提交。

Naive 独有提交：

- `8563441`：Naive padding protocol v1，修改 `forwardproxy.go`。
- `d62c80d`：Caddy/Naive 构建工作流。
- `c096d6a`：2026-10-06 加入 QUIC BBRv1 构建补丁。

Caddy 上游的 6 个提交涉及 README、CI 和 `go.mod`/`go.sum`（包含 quic-go、glog 等依赖更新），这部分差异没有修改 `forwardproxy.go`。

在临时克隆中，将 `caddyserver/master` 合并进 `klzgrad/naive` 无文本冲突。**未完成编译或协议测试**：当前 shell 没有可用的 `go` 命令。无冲突并不等于依赖组合或服务端构建已经验证兼容。

## GitHub 操作与 remotes

在 klzgrad 仓库页面创建 `Na2CuCl4/forwardproxy`，确认复制了 `naive` 分支。只复制默认分支即可满足开发需要，不必导入陈旧 `master`。

用户计划自行 clone 至 `/home/ubuntu/forwardproxy`。SSH 认证就绪后，可执行：

```bash
git clone --branch naive git@github.com:Na2CuCl4/forwardproxy.git ~/forwardproxy
cd ~/forwardproxy
git remote add upstream https://github.com/klzgrad/forwardproxy.git
git remote add caddy-upstream https://github.com/caddyserver/forwardproxy.git
git fetch upstream
git fetch caddy-upstream
git switch -c codex/usage-accounting
```

若当前机器尚未配置 GitHub SSH 登录，可将 clone URL 换成公开仓库的 HTTPS URL；这只解决公开读取，推送仍需适当授权。

- `origin`：自己的 fork，保存本项目改动。
- `upstream`：直接跟踪 klzgrad 的 Naive 更新。
- `caddy-upstream`：观察 Caddy 通用修复及依赖更新。

维护时优先跟踪 Naive 上游。对于 Caddy 上游需要提前引入的修复，建立独立集成分支，审查、测试后再合入；不要仅为消除 GitHub 的 behind 提示直接同步生产。保留纯 Naive 基线，方便比较自己的计量改动；共享分支不自动 force push。

首次 fork 后不需要立即合并 Caddy 的 6 个提交。应先建立可复现的 Naive 构建基线，再独立评估依赖同步，最后添加统计功能。

## 构建上的重要区别

最新 `c096d6a` 的 `CADDY_VERSION` 为 `v2.11.7`。官方 build workflow 除了编译 forwardproxy，还会：

1. checkout 指定版本的 Caddy 源码；
2. 应用 `caddy-sing-quic-bbrv1.patch`，替换 QUIC 实现并接入 sing-quic BBRv1；
3. 用 xcaddy 同时替换 Caddy 与 forwardproxy 模块。

因此，普通 `xcaddy --with ...` 不会自动执行仓库里的工作流，也不会自动应用这份 Caddy 补丁。需要区分“保留 Naive padding 的普通构建”与“完整复现最新 klzgrad release 的 HTTP/3/BBR 构建”。对于现在使用 HTTPS 的节点，不应把这次 QUIC 构建变化与流量统计功能捆绑上线。

不需要立即另外 fork 整个 Caddy：可先保留 upstream 的补丁构建方式。发布时固定 Caddy、forwardproxy、工具链和相关依赖版本/提交，进行 Naive 客户端互通、上传下载、长连接、reload 及计量去重验证。

保留模块路径 `github.com/caddyserver/forwardproxy`，沿用 upstream 的 xcaddy replacement 用法；首次 fork 不要仅因仓库归属改变就全量重命名 Go 模块和内部 import。

官方工作流当前只在 `naive` 分支 push 或 release 时触发；未来自己的功能分支与 PR 检查需要补充 CI。不要把上游 release 标签名当作本项目新版本，应为自己的构建使用独立版本标记并记录基线。

## 参考

- https://github.com/klzgrad/naiveproxy#server-setup
- https://github.com/klzgrad/forwardproxy/tree/naive
- https://github.com/caddyserver/forwardproxy/tree/master
- https://github.com/klzgrad/forwardproxy/commit/c096d6a00cb28e019cc1995b04bdc6a9311d8024

