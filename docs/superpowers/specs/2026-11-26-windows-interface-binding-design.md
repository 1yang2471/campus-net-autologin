# Windows 接口级绑网与代理共存根治设计（2026-11-26）

## 1. 背景与问题定义

2026-11-26 的故障中，校园门户会话始终在线，但物理锚点与用户路径同时失败，公网出口在约 45 分钟后恢复。故障起点与 FlClash 写入数据相邻，但门户在线说明这不是认证态丢失。

代码层确认了一个独立缺口：Windows 的“物理栈”目前只设置 `net.Dialer.LocalAddr`，没有绑定接口索引。源 IP 绑定不能阻止路由型 TUN 接管 socket；DNS UDP、门户 TLS 探测和 `Bindable` 自检也各自存在只绑 IP 的路径。

本设计解决的是本地网络栈缺口，不宣称能够推翻校园 AC/DPI 策略，也不宣称能够绕过所有 WFP 按进程重定向。

## 2. 目标与非目标

### 目标

- Windows 物理栈的 TCP、DNS、TLS 探测和可绑定性自检真正绑定源 IP 所属接口。
- 默认严格：接口绑定失败时不发送凭据，不自动回退系统栈。
- 保留显式降级和非 Windows 现有行为。
- 实现仅本机回环的 Clash/Mihomo fake-ip flush，失败不阻断认证。
- 状态与日志可区分 `interface`、`source_only`、`system`，避免把降级路径冒充为已根治。
- 发布 `v2.4.2-go` Windows onedir zip，并同步文档与验收证据。

### 非目标

- 不修改系统路由、网卡 metric、代理开关或 WFP 规则。
- 不引入驱动、Helper 服务、管理员权限或 Windows 签名要求。
- 本期不发布 Android，不改变 Android 的进程级绑网语义。
- 不解决校园 AC/DPI 限制窗，不承诺所有游戏加速器都能被接口绑定绕过。

## 3. 关键决策

1. 使用 Windows `IP_UNICAST_IF` 与 `IPV6_UNICAST_IF`，在 socket connect/bind 前设置接口。
2. 每次拨号按源 IP 重新解析接口，不缓存接口索引，适配同 IP 重连和网卡重建。
3. `strict_interface_bind = true` 为 Windows 默认值；关闭后允许旧式仅绑源 IP 的降级路径。
4. `net_mode=system` 仍是显式专家覆盖；严格策略只阻止 `auto` 的自动回退，不覆盖用户显式选择。
5. 严格模式下，门户解析与凭据路径不得通过系统 DNS 或系统栈偷偷回退；L2 的系统解析仅作为诊断对照。
6. Clash API 只接受本机回环 HTTP 地址，只调用 `POST /cache/fakeip/flush`，不调用会断开全部连接的接口。
7. 版本使用 `v2.4.2-go`，仅发布 Windows zip；Android 资产保持不变。

## 4. 架构设计

### 4.1 统一物理拨号入口

在 `internal/netstack` 增加统一入口，为 TCP、UDP 和 Listen 生成三件一致的绑定事实：

- 源 IP：作为 `LocalAddr`。
- 接口索引：由源 IP 精确匹配本机单播地址得到。
- 接口绑定 Control：Windows 在 socket 创建后、bind/connect 前执行。

源 IP 解析规则：

- `nil`、找不到接口、接口 down、索引为 0、同 IP 出现在多个接口时失败。
- IPv4 使用接口 `IfIndex`；IPv6 使用 `Ipv6IfIndex`，为零时回退 `IfIndex`。
- 不按接口名猜测，不按默认路由猜测，不取第一个匹配。
- 源发现仍按现有“门户可达性”行为选源，不改成仅按网卡结构选择。

### 4.2 Windows 平台实现

新增 `bind_windows.go` 与 `bind_other.go`：

- Windows 使用现有 `golang.org/x/sys/windows`，不新增第三方依赖。
- IPv4：`IPPROTO_IP + IP_UNICAST_IF(31)`，接口索引按网络字节序转换。
- IPv6：`IPPROTO_IPV6 + IPV6_UNICAST_IF(31)`，接口索引使用主机字节序。
- Control 返回错误时终止拨号，不允许退回无接口绑定。
- 非 Windows 返回 nil Control，保持现有 `LocalAddr` 行为。

IPv4 字节序以 WireGuard 的 Windows 实现为参照：`binary.BigEndian` 写入后再按小端 Windows 主机解释；IPv6 不做该转换。

### 4.3 覆盖范围

以下路径必须使用同一绑定实现：

- `netstack.Client` 的 TCP HTTP 拨号。
- `ResolveBound` 的 DNS UDP 拨号。
- `ProbePortal` 的 DNS 与 TLS 拨号。
- `Bindable` 的 UDP Listen 自检。
- IPv6 Peer 的物理栈客户端。
- CLI rebind、首跑探测、多通道探测与登录产生的物理客户端。

以下路径保持不绑接口：

- `NewSystem` 与 `ResolveSystem`。
- 用户路径系统栈观察。
- 显式 `net_mode=system`。
- 本机回环状态 API。

### 4.4 严格、降级与错误分类

新增稳定错误 `netstack.ErrInterfaceBind`，包装接口名、索引和底层错误，但不得记录凭据。

- 严格模式：`auto` 遇到接口绑定失败时 fail-closed，不转系统栈；手动登录和首跑确认使用相同规则。
- 降级模式：保留当前仅绑源 IP 的行为，状态明确标记 `source_only`，日志记录一次降级原因。
- `net_mode=bound` 始终保持 fail-closed。
- DNS 阶段的接口绑定错误映射为源不可用，触发现有清 TTL、重发现和 SourceStale 通知，不误报为普通 DNS 失败。

### 4.5 DNS 边界

- 严格模式下，门户目标解析只接受 `ResolveBound` 成功；校园 DNS 必须是 IP 字面量。
- 不得用系统 DNS 的 fake-ip 作为严格模式凭据路径的 pin。
- L2 诊断可保留系统解析对照，但 TLS 拨号仍走物理接口。
- 非严格模式保留现有回退，并在状态中可观察。

### 4.6 Clash/Mihomo API

新增有界模块，职责仅为 flush fake-ip：

- 配置：`clash_api_url`、`clash_api_secret`。
- URL 仅允许 `http`、本机回环主机、无用户信息、无查询串和 fragment。
- 请求：`POST /cache/fakeip/flush`，可选 `Authorization: Bearer`。
- 超时 2 秒；Transport 显式禁用代理环境变量。
- 仅在登录或恢复成功后触发；失败写告警，不改变认证结果。
- 不记录 secret，不读取订阅内容，不调用 `/connections`。

### 4.7 可观测性

状态与日志新增：

- `bind_mode=interface|source_only|system`。
- `iface` 与 `if_index`。
- 接口绑定失败原因与是否降级。
- Clash flush 的成功、禁用和失败原因。

状态字段不得改变现有登录判定，只用于展示和诊断。

## 5. 测试策略

### 5.1 先写失败测试

- 源 IP 到接口索引：正常、nil、找不到、多匹配、down、索引 0、地址族不匹配。
- Windows socket 选项：IPv4 网络字节序、IPv6 主机字节序、`GetsockoptInt` 回读。
- 绑定失败：服务端零 accept、DNS 服务端零查询。
- 环回 TCP/DNS 集成：源地址正确且系统栈不受接口绑定器影响。
- 严格模式：`auto` 不转系统栈、手动登录与首跑确认零凭据回退。
- 降级模式：显式开启后才走 `source_only`，状态可观察。
- Clash API：回环校验、路径、鉴权、非 2xx、超时、代理环境隔离、secret 不泄露。
- 跨平台编译：Windows、Linux、Android 共享包不破坏。

### 5.2 真机验收

- FlClash TUN 开/关，系统代理开/关。
- 仅有线、仅 Wi-Fi、双网卡同时在线。
- 拔插网线、WiFi 重关联、DHCP 同 IP 重连、休眠唤醒。
- 门户在线但公网异常时，自动登录请求为 0。
- 接口绑定失败时，凭据请求为 0。
- Clash API 未配置、已配置成功、配置错误三种路径均不阻断认证。

单元测试与 socket option 回读不能单独证明“不会被 TUN 抓”，真机 A/B 必须保留记录。

## 6. 发布与文档

- 更新根 README、`go-cnal/README.md`、FAQ、兼容性报告模板。
- 新增当日故障复盘与发布评估，保留历史文档原貌。
- 更新 `scripts/build.ps1` 默认版本为 `2.4.2-go`。
- 根 README 已接近 20KB 文件门，必须删除过时内容或拆分，不能只追加。
- 构建、安装并运行 `release_verify.py`，记录 zip SHA-256。
- 提交、推送 `release`、创建附注 tag `v2.4.2-go`、发布 GitHub Release，仅上传 Windows zip。

## 7. 验收标准

- `go vet ./...` 与 `go test ./... -count=1` 全绿。
- Windows 真机证明物理 HTTP 与 DNS 使用指定接口，TUN 开关不改变该接口。
- 严格模式下接口绑定失败时零凭据出包。
- Clash API 仅回环、失败不阻断认证，文档与实现一致。
- `release_verify.py` 全绿，最终资产哈希可核对。
- GitHub Release 的 tag、资产名、大小和 digest 与本地最终产物一致。
- 未覆盖的 WFP 按进程重定向和校园 AC/DPI 边界在发布说明中明确写出。
