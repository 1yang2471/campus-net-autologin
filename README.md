# campus-net-autologin

**ac_portal 系校园网自动掉线检测与自动重连守护（Windows 托盘常驻）**

掉线后秒级自动补登录（典型 ≤4 秒；最坏 ≈ interval + (锚点数+1)×探测超时 + 登录超时，默认配置约 18 秒），全程无感，彻底告别"填好账号密码还要手动点一次登录"。
默认不绑定学校（门户地址由向导探测或手填），ac_portal 系异校差异改 `config.ini` 即用，附离线兼容性自检工具。

## 本仓库结构

| 目录 | 版本 | 平台 | 说明 |
|---|---|---|---|
| [`go-cnal/`](go-cnal/) | Go v2.2.0-go | Windows | 主实现：同一内核跑托盘/无头/Android 三种壳。`go test ./...` 跑测试 |
| [`go-cnal-android/`](go-cnal-android/) | Kotlin M1 | Android | Go 内核的 Android 壳（AAR + 前台服务）。`./gradlew :app:testDebugUnitTest` 跑测试 |

Go 版与 Android 版只在本校（ac_portal 族）验证过，异校未实测。
各目录有自己的 `README.md` 与 `AGENTS.md`，进去看对应说明。

> 🇬 [English quick start](#english)

---

## 为什么需要它

校园网最常见的三个烦：

1. **偶尔掉线，要重开浏览器手动登录**——计费系统会话老化/设备互踢
2. **买了流量包后必掉一次**——计费档案切换时服务端主动终止会话
3. **挂着代理/加速器时掉线后应用瘫几分钟**——中间层缓存了死连接和旧状态

本工具的答案：**把恢复时间压缩到秒级，且探测不被代理软件蒙蔽**。

| 能解决 | 说明 |
|---|---|
| ✅ 掉线后手动重登 | 自动补发登录请求，不经过浏览器 |
| ✅ 掉线了你不知道 | 后台 3 秒一探（可调 1~300 秒），托盘颜色随时可见状态 |
| ✅ 代理软件（Clash/TUN）下探测失真 | 物理栈直连探测，见下文「物理栈原理」 |
| ✅ 双网卡时另一张卡未认证却看不出 | 面板「通道」行逐卡显示认证态（接口名 + IP + 已认证/未认证/不通/不可达，60 秒一探、零凭据），并注明 Windows 可能误报「无 Internet」；`multi_channel = true` 可让未认证的卡自动补登（与主链路共用账号级登录预算，默认关，只观察） |
| ✅ 首跑要查学校 DNS/服务器地址 | 连上校园网自动探测门户并填入；面板可改 14 项常用参数并即时生效（端点路径/词表等改 config.ini，需重启） |
| ✅ 夜间免认证时段误判掉线 | 零配置自动检测（v1.0.14）：锚点绿 + 连续 2 次登录不受理 = 门户不维护会话 → 入中性免会话态停自动登录，L1 变红自适应退出，不撞门户反暴力冻结。也可用 `free_windows` 手工配置 |
| ⚠️ 服务端踢人本身 | 无法阻止服务端踢你，但秒级自动恢复，体感即"无感" |
| ⚠️ 出口 DPI 风控窗口 | 只观察告警，不干预（见「四层闸机」） |
| ⚠️ 多设备上限 | 参考校无线一个账号可关联 4 个硬件地址；实测多设备会话相互独立（手机重登不影响电脑），超上限才互踢 |
| ⚠️ 手机端 | 已立项（Go 引擎跨端，见 `docs/superpowers/`）；本版仍仅 Windows。临时思路：MacroDroid/Tasker 在"连上校园网 WiFi"触发器里重放登录 POST |
| ⚠️ IPv6 会话 | **双栈补绑已支持，默认关**：门户登录是双栈协议——登录体带 `ipv4or6=<你在另一栈的地址>` 时一次认证同时绑定 v4/v6（门户前端 `logic_new.js` 注释原文「ipv4,ipv6一次认证」）。`dual_stack = true` 时守护重登前现场握手对端并自动携带该字段，踢线后 v6 随重登回真；**对端入口与操作名是校事实**，异校需在 config.ini 配 `v6_peer`/`peer_opr`（留空 = 功能关闭，零影响）。参考校实测：补绑后 v6 `isOnline` 立刻转真，且普通重登不会把它冲掉（2026-11-06）。已知缺口：面板的 v6 读数**仍不绑物理源**，FlClash 这类 TUN 代理在跑时读的是隧道出口而非校园出口，此时「IPv6 可达」不可信。见 FAQ |

## 四层闸机模型（理解本产品边界的关键）

校园网出口不是一道门，是四层闸机。实测（参考校，2026-11）：

```
L0 线路层  DHCP/IP 分配        —— 与认证无关，永远正常
L1 认证层  portal 会话          —— 本工具的本职：3 秒探测 + 秒级重登
L2 策略层  出口 DPI/SNI 管控    —— 只观察告警，绝不干预（对抗 DPI 属违规）
L3 应用层  CDN/服务自身         —— 与校园网无关
```

**L2 实例**：曾观测到认证会话正常、账号状态正常，但 B站/百度等被出口设备按
SNI 精准 RST、约 15 分钟后恢复（2026-11 的历史观察）。2026-11-18 复核：
日志只能证明「大众站 TLS 被重置」，不能把「DPI/15 分钟」当成每次受限的保证；
多源抖动/本地绑定失效也可能表现为网页打不开。此时
L1 探测看到的是"在线"（探测锚点在白名单内），工具会记录 L2 受限告警但不做
任何绕过——这是有意的边界：**工具解决"登录"，不解决"校方策略"**。
遇到 L2 告警：等待恢复，勿反复注销重登（重登可能延长风控）。

## 物理栈原理（本产品最核心的工程决策）

Clash 系代理（FlClash/Clash Verge 等）的 TUN 模式会**接管系统路由与 DNS**：
实测连校园网 DNS 都返回 fake-ip。朴素的 `requests` 探测经 TUN 出去，问的是
代理节点——校园网真掉了它也不知道（本项目曾因此漏判一次时段切换）。

解法：**探测与登录全部走物理网卡**——

1. 自动发现物理网卡 IPv4（排除 fake-ip 段/loopback/APIPA）
2. socket 绑定该源地址（强制流量出物理栈，绕开 TUN）
3. 域名先用**校园网 DNS**（内网段不被 TUN 劫持）解析成真实 IP，再钉 IP 连接
   （TUN 下域名解析不可用，这是实测踩出来的）

判据：**双信号**。① 物理栈下公网锚点返回 204/非门户页 = 路由通；② 每 60 秒
向门户 `userflux` 要一次流量表 = **会话真值**（按源 IP 认会话：在线拿到表，
被踢只会拿回登录页空表）。路由通但会话死（AC「假在线」，2026-11-05 实测：
游戏流量被掐断、公网锚点却全程绿灯，守护一度无动于衷）时，连续 2 次空表
即判掉线自动重连。flux 端点不适配的门户（cookie 认会话型，查流量恒空表）
会被"判死→重登说在线→又空表"识别为探针假阳性，连续 3 次自动停用探针
1 小时——宁可不探，不对着好会话无限重登。

L2 受限窗口期（网页打不开但认证正常）**会直接显示在面板状态细节行和托盘
提示里**（Go 版文案：「在线｜出口受限（观察：大众站 TLS 被重置；认证正常；
勿反复重登）」），不再只躺在日志——避免用户把出口管控误判为掉线而手动注销
重登（那反而可能延长风控）。托盘与面板读的是同一份文案（单源渲染），不会再
出现面板说一套、托盘只剩"在线"两字的情况。

## 快速开始

要求：Windows 10/11（本产品按 Windows 实测开发；代码里的 Linux 分支仅为兜底保留，未做支持承诺）。

1. 解压 zip 到任意目录；
2. `powershell -ExecutionPolicy Bypass -File scripts\install.ps1`（用户级，不需管理员）；
3. 首次运行 `cnalw.exe` 会自动探测门户、生成最小配置并打开设置面板收集账号密码。

托盘图标：🟢 在线（掉线后秒级恢复期间维持绿）/ 🟠 认证服务器不可达 / 🔴 登录被拒（额度/密码）/ ⚪ 启动中或离线兜底。
右键菜单：**立即检测重连 / 打开面板 / 查流量 / 打开认证页 / 查看日志 / 开机自启 / 退出**。
"开机自启"勾选即用（写当前用户注册表 Run 键，无需管理员；取消勾选即移除）。

## 配置说明（config.ini）

```ini
[keeper]
server =                            # 认证服务器（留空：向导自动探测或手填，不预填任何学校）
username = 你的账号                   # 首次运行引导填入；密码存系统凭据管理器
password = <keyring>                 # 占位——真实密码在 Windows 凭据管理器
probe_ips = 223.5.5.5,119.29.29.29   # L1 公网探测锚点（IP 直连，绕 DNS）
campus_dns =                         # 校园网 DNS（留空走系统 DNS；填对则物理栈解析更快更准）
dual_stack = false                   # true = 登录自动握手对端并携带 ipv4or6（E4 一次认证绑两栈；v6 掉线自动回真）
v6_peer =                            # 对端栈入口裸 v6 字面量（如 1::2）；空 = 双栈关闭
peer_opr =                           # 对端握手操作名（校事实词表，异校必配；汕大实测 ipv4or6online）
peer_path = /ac_portal/login.php     # 对端端点路径（校事实，来自门户 JS 字面量）
probe_timeout = 3                    # 探测超时秒数（1~15）
login_timeout = 0                    # 登录超时秒数；0=自适应 max(6, 2×probe_timeout)，慢门户可调大
interval = 3                         # 探测周期秒数（钳制 1~300）
auto_reconnect = true                # false=只探测报状态、掉线不自动登录（面板"掉线自动重连"开关；"立即检测重连"按钮仍可单次手动触发）
multi_channel = false               # true=未认证的其它网卡自动补登（面板「多通道自愈」开关；与主链路共用账号级登录预算，主链路掉线时先修主）
free_windows =                       # 免认证时段（如 23:30-06:30，逗号分隔多段）：该时段网络无需登录，守护不判死不自动重登，面板显示"免认证时段"
reject_backoff = 600                 # 硬错误（密码/额度）退避秒数（60~86400）
transient_backoff = 30               # 瞬态错误（时段切换竞态）短退避（5~600）
portal_down_wait = 30                # 认证服务器不可达时等待秒数（5~600）
ssl_verify = false                   # 自签证书门户保持 false
bind_ip =                            # 高级：手动指定物理网卡 IP（默认自动发现）
clash_api_url =                      # 可选：mihomo 外部控制器（仅本机 loopback）
clash_api_secret =                   # 控制器密钥（没设留空）
login_page = /ac_portal/default/pc.html?tabs=pwd
login_path = /ac_portal/login.php     # 登录 POST 端点（异校不同路径时改这里）
flux_path = /ac_portal/userflux       # 查流量端点（异校不同路径时改这里）
probe_path = /generate_204            # 在线探测路径（锚点不提供此路径时改这里）
probe_host = connect.rom.miui.com     # 204 探测的 Host 头（自定义锚点且校验 Host 时改）
user_agent = Mozilla/5.0 ...         # 部分门户校验 UA

[accounts]
# 多账号历史（面板自动维护，也可手改）。序号=最近使用顺序（1 最新），
# 值=账号|服务器；切换在面板「历史账号」下拉选中即切，当前账号以
# [keeper] username 为准。
# 密码**不进 ini**——按用户名存 Windows 凭据管理器（同主凭据机制），
# keyring 不可用时该历史账号切换会提示补密，不会拿旧账号密码去登。
# 1 = 20231234|https://portal.example.edu
# 2 = 20231235|https://portal.example.edu

[form]
# 各校 ac_portal 表单差异字段（可选），会覆盖/扩展默认表单 opr=pwdLogin。
# extra_field = value

[response]
# 异校响应词表（可选，逗号分隔）。默认覆盖参考校实测形态；
# 只换词典、判定结构不变。改完用 cnal compat 验证，详见「异校适配」。
# success_marks = 成功,登录成功,login_ok
# negative_marks = 未成功,不成功,失败,错误,error,fail
# transient_marks = noauth,not taken,请求异常,空响应,timeout,timed out,无法判定
# hijack_marks = ac_portal,pwdLogin
```

**配置文件位置**：`%LOCALAPPDATA%\campus-net-autologin\`（config.ini / keeper.log /
keeper.lock 都在这，与源码/exe 共用同一份）。改完 config.ini 需重启生效
（配置只在启动时读取）；也可在面板设置里改账号密码服务器并即时生效。

## 适用范围与异校适配

**本工具默认不绑定任何学校**：`server` 留空由向导自动探测或手填，`campus_dns`
留空走系统 DNS。默认端点/表单/响应词表按 ac_portal 门户（参考校实测）
设定，异校差异全部通过 `config.ini` 调整，**无需改代码**。

**边界先说清**（2026-11-04 公开抓包/开源脚本调研）：校园网门户是**多协议
割裂**的市场——ac_portal / eportal（Dr.COM、锐捷等）/ 深澜 Srun 等族的
端点、表单、登录流程互不兼容。本工具只自动化 **ac_portal 族的「朴素变体」**：
明文密码、无 auth_tag（参考校实测可用）。带 auth_tag（一次性令牌）或密码加密
的 ac_portal 变体、以及 eportal/Srun 等异协议族**不在范围**——流程级差异，
改 config.ini 无法适配。另外一条价值边界：**本工具只在掉线时起作用**——
计费系统普遍设有空闲超时（如 2 小时无流量自动断）、时长上限（如心跳到期）、
设备顶号、流量包切换等强制下线机制，掉线频繁的学校收益最大；如果你的学校
开着无感知认证、几乎不掉线，keeper 只会周期探测、不做任何事（无坏处也无收益）。
另：登录响应仅解析前 2KB——成功标记落在其后的超长成功页会判「无法判定」
（瞬态短退避重试，不会误判成功），属已知边界。

异校第一步，用协议指纹离线判定（0 凭据、不联网）：

```bash
cnal compat --detect <登录页样本>      # 文本或 --file page.html
```

输出三态结论：**适用**（朴素 ac_portal）/ **变体暂不支持**（auth_tag/密码
加密）/ **不适用**（eportal/Srun 等异协议或特征不明）。

适用前提（满足其一即可试）：

- 认证页 URL 含 `/ac_portal/`
- 登录为 `{server}` + 可配 `login_path` 的 POST，表单含 `opr=pwdLogin`

不是这类门户？改端点未必够（流程不同），请用 `--detect` 确认后另寻工具。

### 异校适配清单（先用 --detect 判族，再按需逐项）

0. **协议指纹**：`cnal compat --detect <登录页样本>`——不是朴素
   ac_portal 就此打住，别浪费时间改配置。
1. **门户地址**：`server` 填你校认证地址，或用向导自动探测。
2. **端点**：`login_path` / `flux_path` / `probe_path` 若与默认不同，逐项改。
   `flux_path` 只影响「查流量」，不影响重连本职。
3. **表单差异**：`[form]` 段加/覆盖字段（仅限"多带一个普通字段"的形态；
   需要取页面令牌或先加密密码的流程级差异不在可配范围）。
4. **校园 DNS**：`campus_dns` 填 `ipconfig /all` 里网卡的第一行 DNS；留空也能
   用（系统 DNS 回退），填对则物理栈下解析更快更准。锚点 `probe_ips` 可按需换。
5. **响应判定**：抓一次登录响应文本（浏览器 F12 / 抓包），用离线自检验证：
   ```bash
   cnal compat "把登录响应文本粘在这里"
   cnal compat --file response.txt
   ```
   输出会告诉你会被判成「成功 / 拒绝 / 瞬态」以及命中哪条规则。**不联网、
   不发凭据**，可放心在校外先验证。判定与实际不符时，在 `[response]` 段
   覆盖词表后重跑，直到判对。
6. **双栈补绑（可选，默认关）**：`dual_stack = true` 后重登前现场握手对端、
   自动携带 `ipv4or6`（一次认证绑两栈）。需另配两个**校事实**键：`v6_peer`
   （对端栈入口裸 v6 字面量）与 `peer_opr`（对端握手操作名，汕大实测
   `ipv4or6online`）——从你校门户登录页引用的 JS 里找，暂无自动提取；
   两键留空 = 双栈关闭，异校零影响。

> 判定结构（标准 JSON / 伪 JSON / 长短响应分治 / 防误判锚定）是刻意保留的
> 护栏，不参数化；异校只需换词表。参考校用户无需任何 `[response]` 配置。
> 查流量按源 IP 认会话是参考校实测校准，异校可能返回空表——不影响重连本职。

### 实测名单（随社区报告更新；报告模板见 `.github/ISSUE_TEMPLATE/`）

| 学校 | 门户地址 | --detect 结论 | 实测结果 | 来源 |
|---|---|---|---|---|
| 参考校 | portal.example.edu | compatible | ✅ 长期日用 | 作者实测 |
| （等你的报告） | | | | |

> `go-cnal` 的模拟门户回归（三方言端到端 + 变体诚实失败）证明的是
> 机制级通用性；某校真实可用与否以本表为准。

## 代理与加速器共存

keeper 自身与代理软件**正交**（探测/登录走物理栈，见上）：

- **TUN 模式**：物理栈直连不受 TUN 劫持；**坑**：IP 直连 10.0.0.1 会被代理到真
  Cloudflare——本工具全程用域名 + 校园 DNS 解析，不踩此坑
- **系统代理模式**：`trust_env=False`，不受影响
- **游戏加速器（UU/雷神等）**：闭源黑盒无接口；3 秒断网窗口通常吃不掉加速器心跳
  周期，恢复快一个数量级；个别仍瘫的应用重开加速器即可（诚实边界）
- **Clash API 联动（可选）**：配 `clash_api_url`（仅接受本机回环 `http://127.0.0.1[:port]` / `http://localhost` / `http://[::1]`）
  后，恢复成功时自动 flush fake-ip + 清死连接，代理侧应用秒级恢复。默认关——
  多数用户不开外部控制器，不配即靠代理软件自身心跳恢复（通用主路径）

## 常见问题

见 [docs/faq.md](docs/faq.md)（3 秒节流、被踢、IPv6、改配置、手机端等）。

## 安全说明

- 密码存 **Windows 凭据管理器**（keyring），config.ini 只留占位；异常环境自动
  降级明文并日志告警。凭据不提交仓库（已 .gitignore）
- 不绕过任何计费/策略限制，仅自动化你本人账号的正常登录动作
- 单实例锁：重复启动会自动退出，防双开互踢

## License

MIT

---

## English

**campus-net-autologin** keeps you online on `ac_portal`-based campus networks
(ac_portal-based portals). It probes every 3 seconds over
the **physical network stack** (immune to Clash/TUN DNS hijacking),
re-authenticates automatically within seconds after a drop, and observes
egress DPI policy windows without interfering. Passwords live in the Windows
Credential Manager. Tray app with autostart toggle.

```bash
# Extract the zip, then:
powershell -ExecutionPolicy Bypass -File scripts\install.ps1
# First run of cnalw.exe auto-detects the portal and opens the settings panel.
```

MIT License. Chinese documentation above is authoritative.
