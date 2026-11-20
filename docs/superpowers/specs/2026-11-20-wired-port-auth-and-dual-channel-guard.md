# 有线口认证机制与双通道守护升级方案（2026-11-20 实测）

> 背景：用户反馈「插网线没反应、图标不变」，同时长期「打 LOL 卡、校园网用一半断」。
> 本次在汕大校园网完成了分段定位、有线口认证打通与主通道切换，并整理升级经验。
> 状态：**探索性记录 + 升级方案**，尚未改动 keeper 代码。

## 结论速览

- 有线口不是坏了：链路层完全正常（1Gbps、DHCP 正常、网关可达），缺的是 **Web 认证**；认证后同一时刻实测「有线→腾讯 50 发 0 丢包、平均 12ms」，优于 WiFi（同靶 4% 丢包、16ms）。
- 桌面侧动作已完成：`dot3svc` 启动（无害）、以太网跃点调为 25（主通道）、拔线由 Windows 自动回退 WiFi（35）。
- 升级方向：keeper 从「单通道（WiFi）」扩为「**按网卡绑定源 IP 的通道化探测 + 重认证**」；ac_portal 协议本身可完全复用。

## 一、有线口形态（汕大实测）

1. **链路层**：Realtek PCIe GbE，插线后 Link Up、DHCP 下发内网地址（`<有线口DHCP地址>/24`）、网关可达（`<有线网关>` 1ms）。
2. **认证层**：**不是 802.1X**。`netsh lan show interfaces` 返回 `Network does not support authentication`；认证走 **ac_portal Web 门户**（与 WiFi 同一套门户，但**会话按源 IP 独立**）。
3. **未认证表现**：ICMP/TCP 对公网全黑洞（ping 0 应答），HTTP 被 AC 劫持 **302** 到：
   `https://a.stu.edu.cn:443/ac_portal/proxy.html?template=20170602150308&tabs=pwd&vlanid=0&url=...`
4. **认证后**：IPv4 全通（实测外网 200 / 腾讯 0 丢包）。

## 二、门户入口与登录协议（本次摸清）

- `a.stu.edu.cn` DNS：A = `1.1.1.2, 202.192.159.2, 202.104.245.186, 202.192.144.20`；AAAA = `2001:250:3003:20::2, 2001:250:3003:12::154`。
- **未认证的有线源只能连通 `1.1.1.2`**（其余 A 记录的 TCP 连接被拒）。纯 IPv4 登录入口：
  `http://1.1.1.2/ac_portal/20170602150308/pc.html?template=20170602150308&tabs=pwd&vlanid=0`（实测 200）
- 登录接口：`/ac_portal/login.php`，表单字段（自 `logic_new.js` 提取）：
  `{opr: "pwdLogin", userName, pwd, ipv4or6, rememberPwd}`
- JS 内其他操作名：`logonipv4or6`（ajaxLogonOtherIp）、`ipv4or6online`（ajaxCustom）——与 go-cnal 已有校事实（`ipv4or6online`）一致。
- **会话模型**：门户按「来源 IP」绑会话。同一账号在 WiFi 已认证**不会**放行有线口 IP；必须从有线源 IP 完成一次登录。

## 三、本次暴露的「浏览器假象」（接入守护时必须防）

1. 浏览器优先 IPv6 → 走 WLAN 已认证会话 → 门户显示「已登录」，**永远看不到有线登录页**；用户由此误判「一直已登录」。
2. 破法：用 **IP 字面量 URL**（无 IPv6 可选），或命令行绑定源 IP 验证：
   `curl -4 -s --interface <有线源IP> -o /dev/null -w "%{http_code} %{redirect_url}\n" http://www.msftconnecttest.com/connecttest.txt`
   - 302 + `ac_portal` = 未认证；200 = 已认证；全超时 = 黑洞/故障。
3. 同理，keeper 的「在线判定」必须**绑定该通道源 IP** 探测，不能用默认路由（会测到另一条通道的会话）。

## 四、双通道守护升级方案（keeper）

目标：有线为主、WiFi 兜底；任一通道掉认证各自自愈；不插线时零影响。

1. **通道枚举**：取「状态 Up + 有单播 IPv4 + 有默认网关」的物理网卡，排除虚拟/VPN/TUN 适配器。
2. **通道探测**：绑定该通道源 IP，GET `http://<探测地址>/connecttest`（或校园探测 IP），按 §三 判 online / unauth / blackholed / offline 四态。
3. **通道登录**：复用现有 ac_portal 协议，`bind_ip = 该通道源 IP`；DNS 用该通道的校园 DNS 或直接钉门户 IP（`1.1.1.2`）。
4. **节流与退避**：沿用 `portal_down_wait`、探测间隔、指数退避；两通道独立状态机、日志按通道打标。
5. **不改跃点策略**：Windows 原生行为即可——有线插着时默认路由走有线；拔线后接口 Down、路由消失，WiFi 自动接管（实测：WLAN 35 / 以太网 25，回退无感）。如需钉优先级，`Set-NetIPInterface -InterfaceAlias 以太网 -InterfaceMetric 25`。
6. **纪律不变**：凭据只走 keyring；探测/登录走物理栈绑定源 IP（绕开 Clash/FlClash TUN）；不碰系统代理。

## 五、排障方法论（本次沉淀，可复用）

1. **分段 ping**：网关 → 宿舍柜机 → 校园核心 → 出口路由 → 互联点 → 目标，每段 25–50 发，找「第一个丢包点」。
2. **对照实验**：同机换通道（手机热点 vs 校园网）测同一目标；**时段、目标必须一致**，否则结论不成立（本次曾因跨时段/跨目标导致对照偏弱）。
3. **无效测试目标**：不回 ICMP 的 IP（如 `121.51.90.1`）不能当丢包证据；`tracert` 中间跳超时 ≠ 丢包（路由器普遍不回 TTL 超时）。
4. **事件日志**：`WLAN-AutoConfig` 的 8003 断线要按「原因」分类——「网络被驱动程序断开」才是真实掉线；「用户想要建立新连接」多为主动切网（本次 91 次断线中仅 8 次为驱动级）。
5. **浏览器假象**：IPv6 优先 + 已认证会话会掩盖未认证的 IPv4 通道（本文 §三）。
6. **每步留回退**：改跃点前记录原值；临时脚本/下载文件用后即清。

## 六、本次系统改动清单（全部可回退）

| 改动 | 现状 | 回退 |
|---|---|---|
| `dot3svc` 服务 | Running / Automatic | `Stop-Service dot3svc; Set-Service dot3svc -StartupType Manual` |
| 以太网跃点 | 25（主通道） | `Set-NetIPInterface -InterfaceAlias 以太网 -InterfaceMetric 100` |
| 有线 Web 认证会话 | 已登录（用户手动完成） | 门户登出或等待会话超时 |

## 七、待办

- [ ] keeper 双通道支持（本方案落地）
- [ ] 有线会话掉线检测与自动重认证纳入守护
- [ ] （可选）go-cnal 同步通道化逻辑
