# campus-net-autologin

**ac_portal 系校园网自动掉线检测与自动重连守护（Windows 托盘常驻）**

掉线后秒级自动补登录（典型 ≤4 秒；最坏 ≈ interval + (锚点数+1)×探测超时 + 登录超时，默认配置约 18 秒），全程无感，彻底告别"填好账号密码还要手动点一次登录"。
默认不绑定学校（门户地址由向导探测或手填），ac_portal 系异校差异改 `config.ini` 即用，附离线兼容性自检工具。

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
| ✅ 首跑要查学校 DNS/服务器地址 | 连上校园网自动探测门户并填入；面板可改 14 项常用参数并即时生效（端点路径/词表等改 config.ini，需重启） |
| ⚠️ 服务端踢人本身 | 无法阻止服务端踢你，但秒级自动恢复，体感即"无感" |
| ⚠️ 出口 DPI 风控窗口 | 只观察告警，不干预（见「四层闸机」） |
| ⚠️ 多设备上限 | 参考校无线一个账号可关联 4 个硬件地址；实测多设备会话相互独立（手机重登不影响电脑），超上限才互踢 |
| ❌ 手机端 | v1 不做；见 FAQ 安卓思路 |
| ❌ IPv6 掉线 / 前缀或出口故障 | **只保障 IPv4 认证会话，IPv6 由系统/路由器/代理管理**（全链路纯 IPv4；v6 故障重登救不了，见 FAQ） |

## 四层闸机模型（理解本产品边界的关键）

校园网出口不是一道门，是四层闸机。实测（参考校，2026-11）：

```
L0 线路层  DHCP/IP 分配        —— 与认证无关，永远正常
L1 认证层  portal 会话          —— 本工具的本职：3 秒探测 + 秒级重登
L2 策略层  出口 DPI/SNI 管控    —— 只观察告警，绝不干预（对抗 DPI 属违规）
L3 应用层  CDN/服务自身         —— 与校园网无关
```

**L2 实例**：曾观测到认证会话正常、账号状态正常，但 B站/百度等被出口设备按
SNI 精准 RST、约 15 分钟后自动放行（多账号并发 + 注销重登触发风控）。此时
L1 探测看到的是"在线"（探测锚点在白名单内），工具会记录 L2 受限告警但不做
任何绕过——这是有意的边界：**工具解决"登录"，不解决"校方策略"**。
遇到 L2 告警：等窗口过期，勿反复注销重登（那会延长风控）。

## 物理栈原理（本产品最核心的工程决策）

Clash 系代理（FlClash/Clash Verge 等）的 TUN 模式会**接管系统路由与 DNS**：
实测连校园网 DNS 都返回 fake-ip。朴素的 `requests` 探测经 TUN 出去，问的是
代理节点——校园网真掉了它也不知道（本项目曾因此漏判一次时段切换）。

解法：**探测与登录全部走物理网卡**——

1. 自动发现物理网卡 IPv4（排除 fake-ip 段/loopback/APIPA）
2. socket 绑定该源地址（强制流量出物理栈，绕开 TUN）
3. 域名先用**校园网 DNS**（内网段不被 TUN 劫持）解析成真实 IP，再钉 IP 连接
   （TUN 下域名解析不可用，这是实测踩出来的）

判据：物理栈下公网探测返回非 302（含 404）= 包真实到达公网 = 已认证；
被门户拦截（302/超时）= 掉线。

## 快速开始

要求：Python 3.10+，Windows 10/11（本产品按 Windows 实测开发；代码里的
Linux 分支仅为兜底保留，未做支持承诺）。

```bash
pip install -r requirements.txt
python app_main.py        # 首次运行引导填账号，生成 config.ini
```

托盘图标：🟢 在线（掉线后秒级恢复期间维持绿）/ 🟠 认证服务器不可达 / 🔴 登录被拒（额度/密码）/ ⚪ 启动中或离线兜底。
右键菜单：**立即检测重连 / 打开面板 / 查流量 / 打开认证页 / 查看日志 / 开机自启 / 退出**。
"开机自启"勾选即用（写当前用户注册表 Run 键，无需管理员；取消勾选即移除）。

**杀毒/SmartScreen 提示**：主分发为纯源码（`python app_main.py`），一般不触发；
仓库另附 `build_exe.py` 可选出包（onedir 非 onefile 降低误报），未签名 exe 首次
运行可能弹 SmartScreen"已保护你的电脑"——点"更多信息 → 仍要运行"即可，只第一次。

## 配置说明（config.ini）

```ini
[keeper]
server =                            # 认证服务器（留空：向导自动探测或手填，不预填任何学校）
username = 你的账号                   # 首次运行引导填入；密码存系统凭据管理器
password = <keyring>                 # 占位——真实密码在 Windows 凭据管理器
probe_ips = 223.5.5.5,119.29.29.29   # L1 公网探测锚点（IP 直连，绕 DNS）
campus_dns =                         # 校园网 DNS（留空走系统 DNS；填对则物理栈解析更快更准）
probe_timeout = 3                    # 探测超时秒数（1~15）
login_timeout = 0                    # 登录超时秒数；0=自适应 max(6, 2×probe_timeout)，慢门户可调大
interval = 3                         # 探测周期秒数（钳制 1~300）
auto_reconnect = true                # false=只探测报状态、掉线不自动登录（面板"掉线自动重连"开关；"立即检测重连"按钮仍可单次手动触发）
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
# 只换词典、判定结构不变。改完用 compat_check.py 验证，详见「异校适配」。
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
python compat_check.py --detect <登录页样本>      # 文本或 --file page.html
```

输出三态结论：**适用**（朴素 ac_portal）/ **变体暂不支持**（auth_tag/密码
加密）/ **不适用**（eportal/Srun 等异协议或特征不明）。

适用前提（满足其一即可试）：

- 认证页 URL 含 `/ac_portal/`
- 登录为 `{server}` + 可配 `login_path` 的 POST，表单含 `opr=pwdLogin`

不是这类门户？改端点未必够（流程不同），请用 `--detect` 确认后另寻工具。

### 异校适配清单（先用 --detect 判族，再按需逐项）

0. **协议指纹**：`python compat_check.py --detect <登录页样本>`——不是朴素
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
   python compat_check.py "把登录响应文本粘在这里"
   python compat_check.py --file response.txt
   ```
   输出会告诉你会被判成「成功 / 拒绝 / 瞬态」以及命中哪条规则。**不联网、
   不发凭据**，可放心在校外先验证。判定与实际不符时，在 `[response]` 段
   覆盖词表后重跑，直到判对。

> 判定结构（标准 JSON / 伪 JSON / 长短响应分治 / 防误判锚定）是刻意保留的
> 护栏，不参数化；异校只需换词表。参考校用户无需任何 `[response]` 配置。
> 查流量按源 IP 认会话是参考校实测校准，异校可能返回空表——不影响重连本职。

### 实测名单（随社区报告更新；报告模板见 `.github/ISSUE_TEMPLATE/`）

| 学校 | 门户地址 | --detect 结论 | 实测结果 | 来源 |
|---|---|---|---|---|
| 参考校 | portal.example.edu | compatible | ✅ 长期日用 | 作者实测 |
| （等你的报告） | | | | |

> `tests_tdd.py` 循环26 的模拟门户回归（三方言端到端 + 变体诚实失败）证明的是
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

**Q：为什么是"3 秒"这么激进？**
A：TCP 重传机制下，3~6 秒窗口内大部分在途连接能靠重传存活；加速器心跳（5~15 秒）
吃不掉 3 秒窗口。interval 钳制 1~300 秒防呆。掉线态每周期逐锚点探测（默认 2 个）
+ 门户确认；登录尝试独立节流（被拒退避 + 连续 3 次熔断），不轰炸门户。

**Q：莫名被踢？**
A：学校限 4 个 MAC 地址绑定，去认证页自助服务解绑闲置设备；手机关随机 MAC。

**Q：登录被拒（额度/余额类错误）后一直红点？**
A：设计行为——退避 10 分钟避免空转；买好流量包后在托盘点"立即检测重连"。

**Q：托盘显示在线，但很多网站打不开？**
A：多半是 L2 出口策略风控（见四层闸机），不是认证掉了。日志会有 L2 告警。
等 10~30 分钟窗口过期，**期间勿反复注销重登**（会延长风控）。

**Q：托盘绿着，但 IPv6 网站/双栈应用不通？**
A：本工具**只保障 IPv4 认证会话**：探测锚点、源地址绑定、门户登录全链路都是
IPv4。IPv4 会话没掉就判"在线"是正确判定。IPv6 前缀下发、出口、路由等故障与
portal 认证无关，重登也救不了——IPv6 由系统/路由器/代理管理，请查本机 v6 地址、
默认路由或上游设备。本工具不做 IPv6 探测、也不做 IPv6 重登。

**Q：不在校园网 / 门户连不上，能先把账号配好吗？**
A：能。首配向导点「保存并立即测试」若联网测试失败，会问「仍要保存凭据吗」——
选「是」即落地，联网后由常驻守护自动重试登录。测试走后台线程，窗口不会卡住。

**Q：只想改服务器/账号，不想重输密码？**
A：密码框留空即可——表示「不改已存密码」，不会覆盖也不会清空。测试凭据时
也会自动用已存密码。面板「保存设置」只更新你改的键，不会回滚手工编辑的
config.ini，也不会把凭据管理器里的密码明文写回 ini。

**Q：改了配置怎么不生效？**
A：面板"网络设置/高级设置"里改并点"保存设置"——**立即生效**；
直接编辑 config.ini 的仍需退出托盘重开（文件只在启动时读取）。
「绑定网卡 IP」只接受合法 IPv4（留空=自动发现）；填 IPv6 会被拦下，
因为守护全链路只走 IPv4（见上面的 IPv6 边界说明）。

**Q：手机端？**
A：v1 仅电脑。安卓思路：MacroDroid/Tasker 在"连上校园网 WiFi"触发器里重放登录
POST（抓包一次即可）；iOS 快捷指令无法在连 WiFi 时自动触发，只能手动点。

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
pip install -r requirements.txt
python app_main.py   # interactive first run, writes config.ini
```

MIT License. Chinese documentation above is authoritative.
