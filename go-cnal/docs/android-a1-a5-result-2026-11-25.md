# 安卓 A1–A5 实测结果 · 2026-11-25 轮（自然 Doze 过夜）

> 本文件是 `android-a1-a5-result-2026-11-22.md` 的续篇（2026-11-25 按轮次拆分，
> 主文档末尾有索引）。节号沿用原编号，不重排。

## 9. 自然 Doze 过夜轮自愈核对（2026-11-25 08:00，仅 MIUI 已验）

> 核对目标：验证 v2.4.1 P0 源失效自愈（commit 8b2fb5c）在自然 Doze 深睡下是否把
> bind 失效收敛到分钟级（§8 主动断网 9 秒回在线的「机制归因存疑」留到本轮）。
> 08:00 定时子会话跑时端口 39365 已被拒（TCP 10061）+ `adb mdns services` 空 +
> 手机 IP ping 100% 丢包，按纪律停止；但 ARP 删后立即回填（`172.29.153.170
> → e4-bc-aa-cc-ef-3b`），判定手机 L2 在线、仅端口变更。用户 08:36 给新端口
> **43187** 重连成功、人工重跑。**本次 adb 连接未触发 force-stop**（logcat 全程
> 无 `Force stopping dev.cnal.m0` / `set debug app`），与 §7 的 01:56 那次不同。
> 前置：白名单 ✓（`user,dev.cnal.m0,10358`）；进程 pid **31553** 跨夜未变
> （= §8 昨晚 19:43 记录值），pidof / ps / meminfo 三者一致。

**cnalw.log 全量分析（窗口 = §8 新版本启动 11-24 19:43:19 → 11-25 08:35:09，
93 行带时间戳 / 82 拍 / 跨度 12 小时 52 分）**：

| 判据 | 数字 | 结论 |
|---|---|---|
| A2 进程存活 | pid 31553 跨夜未变；无「守护已启动」二次 | ✓ **过**（进程层） |
| 拍密度 | 相邻拍间隔 >60s **80 次**（共 81 个间隔）、>900s 13 次、最长 69.5 分钟（07:25→08:35，adb 连接前） | ✗ Doze 下仍大幅拉稀（与 §6/§7 同量级） |
| 在线占比 | 66/82 = **80.5%** | 参考值（受 4.9 小时断窗拖累） |
| bind 失败总拍数 | **15 行**（§6 为 13、§7 为 22） | 须分簇看 |
| bind 失败簇1 | 19:44:21 **1 拍** → 19:44:30 回在线 | ✓ §8 的主动断网事件（9 秒） |
| bind 失败簇2 | 23:20:59 **1 拍** → 23:26:01 回在线（**5 分钟**） | ✓ 比 §7 簇1 的 40 分钟短——**自发 bind 失效 1 拍即愈** |
| bind 失败簇3 | 02:28:18→07:16:23 **13 拍 / 292 分钟（4 小时 52 分）**，中间无一在线拍，07:20:40 才恢复 | ✗ **与 §6「13 拍 5 小时」、§7 簇2「13 拍 4 小时」同量级——长断窗仍在** |
| 其它 | 23:36:07 一次「登录被拒」（`Task 'NOAUTH:*' not taken`），23:41:13 回在线 | D6 家族现象，非本轮主判据 |

**核心判据结论：自愈对「自发 bind 失效」见效（簇2：1 拍 / 5 分钟），对「长断窗」
未见效（簇3：13 拍 / 4 小时 52 分）——但簇3 的成因经 logcat 取证已不是 bind 失效。**

**簇3 成因取证（logcat 全程覆盖 11-25 00:00 起，52 万行，pid 31553 记录逐拍对应）**：

| 时间 | 系统事件（logcat 原文摘录） | 含义 |
|---|---|---|
| 02:04:32 | `PowerGroup: Powering off display group ... lastUserActivityEvent=touch, millisSinceLastUserActivity=300014` | 用户约 01:59 在用手机，02:04:32 息屏（`isDeviceIdle=true`） |
| 02:28:18.272 | `AlarmManager: ... com.miui.powerkeeper check_stationary` → `power.sleep: receive ACTION_CHECK_STATIONARY`、`currentLight =0` | MIUI 电量管家判定「静止 + 遮光」 |
| 02:28:18.289 | `PowerManagerService: acquireWakeLock tag="phoneSleepMode:sleep_enter"` | **MIUI「睡眠模式」进入** |
| 02:28:18.350 | `power.sleep: applySleepConfig index=0 setDataEnabled false setWifiEnabled false` | **主动关 WiFi + 关蜂窝数据** |
| 02:28:18.354 | `MiuiBatteryIntelligence: wifi state changed old state : 3 now is : 0` | WiFi 关闭（state 3=ON → 0=DISABLING） |
| 02:28:18.651 | `wificond: tearDownClientInterface ... wlan0` | wlan0 接口拆除 |
| 02:28:18.724 | `HyperCountryCodeResolver: Wifi network lost: 131` | WiFi 网络丢失 |
| 02:28:18.736 | `auditd: avc: denied { bind } for comm="dev.cnal.m0" ... tclass=netlink_route_socket` | 与 cnalw.log 簇3 首拍同秒 |
| 02:28→07:16 | 13 次 `auditd avc: denied { bind }` + 34 次 `notification_enqueue` + 每拍 `acquireWakeLock tag="cnal-auth-wake"`，无 freeze/kill 记录 | **进程全程存活、每拍在跑、每拍 bind 被拒**——是 WiFi 已关的真实反映 |
| 07:20:35.078 | `power.sleep: receive ACTION_ENTER_DAY` | MIUI 睡眠模式到点自动退出（非用户操作） |
| 07:20:35.105 | `power.sleep: restoreSleepConfig index=0 reason=enter_day setWifiEnabled:true` | WiFi 重开 |
| 07:20:36.170 | `wificond: createClientInterface ... wlan0` → 07:20:38 `DhcpClient.wlan0.KICK` | 接口重建 + DHCP |
| 07:20:40 | cnalw.log `状态: 在线` | **WiFi 重开后 4 秒回在线**（对照 §8 的 9 秒） |

**判据逐项结论（诚实，不冒充全过）**：

- **簇3 不是「源失效自愈未覆盖 Doze」**：WiFi 被系统关掉、wlan0 拆除、蜂窝数据也关
  ——**没有任何物理源可绑**，自愈缝通知 Kotlin 重注入也拿不到可用源。守护的 13 拍
  `bind: cannot assign` 是对「无网」的如实报告，不是缺陷。本轮**不能据此宣称
  自愈未覆盖 Doze**，也**不能宣称自愈已覆盖 Doze**——「WiFi 半睡/IP 被摘」这一形态
  本夜**未出现**（§6/§7 的簇是不是同一成因，当时的 logcat 已不可得，无法回溯核对）。
- **自发 bind 失效的自愈 ✓（簇2）**：23:20:59 单拍 bind 失败 → 23:26:01 回在线，
  无换网事件、无人工干预、pid 未变。比 §7 簇1（40 分钟）明显缩短。但 23:26:01
  的「回在线」拍与 23:20:59 之间隔了 5 分钟——恢复的是「下一拍」还是「下一拍恰好
  碰上 WiFi 恢复」无法从 cnalw.log 区分（`printSource` 不写此文件），归为
  **行为成立、机制存疑**（与 §8 同口径）。
- **A2「进程存活」✓（进程层，本次 adb 未人为中断）/「后台网络会话保持」✗**
  （4 小时 52 分，但成因是 ROM 睡眠模式关 WiFi，非守护能力范围）。
- **A3「断档恢复 1 拍内回在线」✓ 形态符合**：07:20:35 WiFi 重开 → 07:20:40 在线。
- **A4/A5 未涉及**。

**与 §6/§7 的差异（本轮新增认知）**：前两轮把长断窗归因为「Doze 期 WiFi 半睡 +
wlan0 IP 被摘」，本轮 logcat 实证为 **MIUI 睡眠模式（`com.miui.powerkeeper`
`phoneSleepMode`）主动关 WiFi**——是「关」不是「摘 IP」。两者在 cnalw.log 里的
表现相同（`bind: cannot assign`），但解法方向不同：前者是源失效自愈能解的，
后者**不是应用级省电豁免能解的**——见下。

**「无限制」为什么没挡住（08:50 用户反馈「安卓端一直是无限制」后补查）**：

| 证据 | 读数 | 含义 |
|---|---|---|
| `settings get system MILLET_NO_RESTRICT_APP` | `dev.cnal.m0, com.iflytek.inputmethod, …, com.tencent.mm` | 「无限制」在系统层**确实生效**（用户所说属实） |
| `am get-standby-bucket dev.cnal.m0` | `5`（EXEMPTED） | 应用待机桶已是最宽档 |
| `cmd appops get dev.cnal.m0 RUN_ANY_IN_BACKGROUND` | `Default mode: allow` | 后台运行无限制 |
| logcat `PolicyMaker … reason=NO_RESTRICT_APP` | 睡眠模式边缘时段名单内应用照常调度 | 名单豁免的是**进程调度** |
| 02:28:18 `power.sleep: applySleepConfig … setWifiEnabled false` | 全局 WiFi 开关 | **硬件级开关，应用名单管不到** |
| `mSleepState={isUserOff=false foreignSim=0 …}` | 用户未关闭该功能；非境外 SIM | 睡眠模式按「夜间静止 + 遮光」**默认自动**进入 |

**结论**：`MILLET_NO_RESTRICT_APP`（应用无限制）和 `deviceidle whitelist`（Doze 白名单）
是**两道进程级豁免**，都已生效——守护进程确实全程活着。但 MIUI 睡眠模式的
`setWifiEnabled false` 是**整机 WiFi 开关**，不属于任何应用级策略的管辖范围。
**「设为无限制」不是长断窗的解法**，本轮核对前的建议 ① 方向错误，已据实修正。

**没验证到的**：①「WiFi 连着但 IP 被摘」形态下的自愈（本夜未出现该形态，§8 的
「机制归因存疑」仍悬）；② §6/§7 簇的成因是否同为睡眠模式（无当时 logcat，不可
回溯）；③ A5 extra v6 轮换；④ 不同 IP 的换网。**本轮是 MIUI 单机单夜，不代表
其它 ROM。**

**下一步建议（按决定性排序）**：
① **关闭 MIUI 睡眠模式本身**（非应用级设置；`cloud_sleepmode_networkpolicy_enabled=on`
   是其网络策略开关键，具体入口需在手机上确认：设置→省电与电池→睡眠模式 / 或
   「夜间省电」）后重跑过夜轮——这是唯一能验证「长断窗是否全由睡眠模式造成」的
   实验；若关掉后仍出现 4 小时级断窗，才回到「Doze 摘 IP + 自愈未覆盖」假设。
② 给 `printSource` 加 cnalw.log 落盘，让「重注入/重绑」在日志里可见，闭合 §8/§9
   的机制归因。
③ 簇2 类型的自发失效多收集几例再下结论。
④ 若 ① 不可行（用户需要睡眠模式省电），守护侧可做的只是**如实上报「WiFi 被系统
   关闭」**（当前文案 `bind: cannot assign` 把「无网」和「源失效」混为一谈，
   排障时误导——本轮就差点据此误判自愈失败）。

**已落地（2026-11-25，v2.4.1）**：上面 ④ 已实现。`bind` 失效的离线文案现在带现场
判断，三态区分——「本机已无任何非环回 IPv4（网卡均未持有地址）」（系统关了 WiFi）/
「源地址已从网卡消失，但本机仍有其它 IPv4」（接口重建/换网）/「源地址仍在网卡上，
绑定被拒」（源没丢，是绑定被系统拒绝）。实现在 `app.bindHint`（`internal/app/egress.go`），
`TestBindHintDistinguishesScenes` + `TestProbeAnchorBindFailureCarriesHint` 覆盖，
变异检查 2 处（分支对调、拆掉接线）均被抓；`go vet` + `go test ./...` 12 包全绿。
