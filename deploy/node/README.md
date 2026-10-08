# 人工 bootstrap 与受限 SSH 节点工具

`naive-node` 只接受固定 `rpc` 入口和结构化 stdin，不向 SSH 客户端开放 shell、路径、环境变量或任意命令。生产初次部署由节点管理员人工执行；本仓库开发/测试不替换宿主机正在服务的 Caddy。

## 构建和可信文件

在 Dashboard 仓库中执行 `go build -trimpath -o naive-node ./cmd/naive-node`。把二进制人工安装到节点后，先在隔离实例验证与本次固定 forwardproxy/Caddy 基线兼容，再由管理员安排生产切换。以下命令是人工步骤示例，不是自动运行脚本：

```sh
sudo install -o root -g root -m 0755 naive-node /usr/local/bin/naive-node
sudo install -d -o root -g root -m 0700 /etc/naive-node /var/lib/naive-node
sudo install -d -o caddy -g caddy -m 0700 /var/lib/naive-usage
sudo install -o root -g root -m 0600 deploy/node/config.example.json /etc/naive-node/config.json
sudo install -o root -g root -m 0600 deploy/node/Caddyfile.template /etc/naive-node/Caddyfile.template
```

按本机实际二进制位置、配置位置、服务管理方式编辑 `config.json`。只接受绝对路径和字面量回环 IP 管理地址；默认示例是 `127.0.0.1:2019`，也支持 `[::1]:2019`。Caddy 必须启用本机 admin API。不得将 admin API 公开到公网。`restart_command` 是管理员配置的固定 argv，省略时 `restart` 返回 `restart_disabled`。不要使用 shell 包装命令。配置、模板、工具、服务管理程序及其上层目录必须为 root 所有且不可由 group/other 写入；`caddy_config` 可保持 `root:caddy 0640`，使 Caddy 服务用户能够在重启时读取。

**模板是完整 Caddyfile。** 把节点已有域名、证书、其他站点、全局设置完整复制进去，仅在专属 `forward_proxy { ... }` 块中用恰好一行 `# NAIVE_USERS` 替代原有 `basic_auth` 行。其余内容逐字保留。该块不得额外含 `basic_auth`，也不要通过 import 引入额外认证。标记必须独占一行，块的花括号使用通常的空白分隔写法，双引号/backtick 字符串不得跨行。配置中的 `{env.*}`/`{$ENV}` 不应依赖 SSH 登录环境：节点工具只提供固定 PATH/LANG。完整候选 `validate` 成功后才可生效。

模板必须配置 `usage_log /var/lib/naive-usage`，需使用本次添加计量功能的 forwardproxy 构建。**首次接管使用新建空的专属用量目录，先撤销旧人工代理账户，再开始 Dashboard 管理**；不要把旧人工账户或其他 forward_proxy 站点的事件混入该目录，中央只接收入库过的代理身份，未知身份会使整批返回 `usage_invalid` 且不推进游标。Caddy 服务用户拥有该用量目录；受限 SSH 用户没有目录写权限。Caddy `validate` 会创建没有业务事件的 epoch 日志，空文件是正常的。首次应用要求磁盘 Caddyfile 与当前 admin API 返回的配置相符，否则返回 `runtime_drift`，由管理员先解决未保存/不同配置运行问题。

## SSH 账号与提权入口

每个节点使用独立的 Dashboard 控制密钥；GitHub SSH 密钥不用于节点控制。创建只供强制命令使用的 `naive-manager` 账号，不授予其他 sudo 权限。sshd 需要执行登录 shell 来运行强制命令，因此账号可使用 `/bin/sh`，但应锁定密码登录，并用专用 Match 规则拒绝此账号所有转发与 TTY。

`/etc/sudoers.d/naive-dashboard`（root:root 0440，通过 `visudo -cf` 验证）：

```sudoers
naive-manager ALL=(root) NOPASSWD: /usr/local/bin/naive-node rpc
```

这条规则只允许精确参数 `rpc`，**不得**追加 `*` 或授权 `-config`。生产始终使用默认 `/etc/naive-node/config.json`。`-config PATH rpc` 仅供管理员/非 root 的隔离测试直接运行。

将 authorized_keys 放在 root 管理的 `/etc/ssh/authorized_keys/naive-manager`（目录 0755，文件 root:root 0644）。sshd 示例片段：

```sshconfig
Match User naive-manager
    AuthorizedKeysFile /etc/ssh/authorized_keys/naive-manager
    AuthenticationMethods publickey
    PasswordAuthentication no
    KbdInteractiveAuthentication no
    PermitTTY no
    AllowTcpForwarding no
    AllowAgentForwarding no
    X11Forwarding no
    PermitTunnel no
    ForceCommand /usr/bin/sudo -n /usr/local/bin/naive-node rpc
Match all
```

公钥文件每一行同时带限制，替换 `AAA...` 为该节点专用的 Dashboard 公钥：

```text
restrict,command="/usr/bin/sudo -n /usr/local/bin/naive-node rpc" ssh-ed25519 AAA... dashboard-node-1
```

用 `sshd -t` 验证后按节点现有运维流程加载 SSH 配置。验证 SSH 任意客户端命令都会进入固定 RPC，转发和 TTY 被拒绝；`naive-manager` 不能修改工具、配置、模板、sudoers 或公钥文件。不要使受限账号可改写工具再由 sudo 执行。安装更新也始终由管理员操作。

管理员通过可信渠道核对 `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub -E sha256` 的指纹。Dashboard 登记地址/端口/账号、专用密钥引用和该 SHA256 主机指纹；不得自动信任未知 host key。工具会忽略 SSH_ORIGINAL_COMMAND；业务输入仅从 stdin 读取：

```sh
printf '%s\n' '{"action":"status"}' | ssh -T -i node-1-key naive-manager@node.example rpc
```

## RPC 与故障恢复

每次 stdin 只能含一个 JSON 对象，最多 1 MiB。action 是 `status`、`apply`、`usage` 或显式 `restart`。返回一个 JSON 对象，`ok:false` 时仍有稳定 `error` 码并以 exit 1 结束；客户端应解析 stdout，再处理退出状态，不将 stderr 直接展示为含敏感配置的错误。用户名为 1–128 个 `[A-Za-z0-9_-]`，密码为 16–256 个 `[A-Za-z0-9_.~-]`，建议使用独立随机 base64url 凭据。

```json
{"action":"apply","version":1,"users":[{"username":"u_demo_n_one","password":"independent_random_password_0123456789"}]}
```

版本是正的 int64，包含**完整期望用户集合**，而非增量。相同版本/用户名密码/模板内容重试不 reload；同版本不同内容或旧版本拒绝。模板人工变更需要中央分配新版本。撤销全部用户时工具生成不可知的随机拒绝哨兵，避免 forwardproxy 空认证列表开放代理。

每次 RPC 获取非阻塞 `flock`，冲突返回 `node_busy` 可重试。`apply` 在完整候选文件通过 `validate`/`adapt` 后，先 fsync 持久化包含旧配置/版本的事务日志，再原子安装、reload、比对 admin API 与候选 JSON、提交版本、移除事务日志。文件权限和属主在原子替换时保留。reload 失败或运行配置不符时恢复旧文件和旧运行配置。进程中途退出后，下一个 RPC 自动恢复未完成事务；恢复失败保留日志并报 `recovery_failed`/`rollback_failed`，不能声称已生效。state 目录备份包含密码，需与其他凭据同等保护。

`status` 返回已应用版本，检查磁盘摘要及运行 JSON；区分 `healthy`、初次未受管理的 `unmanaged`、`config_drift`、`runtime_drift`、Caddy 不可用。显式 `restart` 仅运行管理员固定 argv，并重新检查健康。reload 不承诺终止已有 CONNECT 或绝对不断线。

## 用量游标与维护

用量 RPC：`{"action":"usage","cursor":{"<32hex-epoch>.jsonl":1234}}`。工具按文件名读取 append-only JSONL，最多返回 1000 条事件；每条为 `{epoch,session_id,user_id,up,down,final,at}`，up/down 是会话累计有效载荷字节，不含 Naive padding/TLS。`user_id` 对应代理用户名，中央添加节点 ID。完整行成功解析后游标才前移；缺文件/截断返回 `usage_cursor_lost`。硬崩溃留下的不完整尾行不入账，工具仍继续读取新 epoch 文件。相同已确认游标可重复读取同样事件。

中央必须在同一个数据库事务内按 `(node,epoch,session_id)` 累计差额入账并持久化响应游标；不能先提交游标再入账，也不能把快照与 final 全量相加。节点工具本身不确认/删除日志，MVP 禁止轮转、截断、清理或复用 epoch 文件。硬重启时最后一分钟尚未持久化的增量可能丢失，UI 应显示采集延迟。

长期运行请监控磁盘空间。当前日志保留只追加；后续可增加具有已确认截止点的归档协议。人工备份不等于中央已确认，**不要**自行删除空 epoch 文件或旧日志：中央 cursor 仍可能引用它们。

## 本地隔离验证

节点单元测试以临时目录、fake Caddy 与回环 admin 服务验证应用/幂等、最后用户撤销、校验失败、reload 部分成功回滚、运行摘要不符、事务中断恢复、串行锁、漂移、JSON 输入与用量重放/批量/partial tail/重启 epoch/坏游标。

```sh
GOMAXPROCS=2 go test -p 1 ./internal/node ./cmd/naive-node
```

真实 Caddy 测试必须使用独立数据/配置目录与端口。不要指向正在服务的 `/etc/caddy/Caddyfile`、admin 2019 或生产 service unit。
