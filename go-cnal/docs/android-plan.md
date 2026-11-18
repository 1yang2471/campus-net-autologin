# 安卓版基础方案

> 2026-11-08。目标：把 Windows 上已验证的守护内核搬到安卓，先回答"能不能活"，
> 再回答"怎么好用"。所有平台约束以官方文档为准（文末链接），未验证项显式标注。

## 0. 结论

**Kotlin 薄壳 + gomobile AAR 内嵌现有 Go 引擎 + 系统 WebView 复用 `panel.html`。**

- 引擎不重写：`protocol` / `dialect` / `keeper` / `netstack` / `api` 是纯 Go，
  直接复用；风暴三闸、双栈四态、已在线哨兵这些踩过坑的语义不重造。
- 壳只做安卓特有的事：选网络并 `bindProcessToNetwork`、前台服务存活、通知、
  Keystore 凭据、WebView 容器。
- **先过 A1-A5 真机门槛**（`../campus-net-autologin/docs/superpowers/specs/2026-11-06-dualstack-and-go-port-design.md` §11）：
  A1 息屏存活、A2 夜间 Doze×OEM、A3 省电×空闲超时、A4 绑网正确性、A5 手机 v6 现实。
  任一条不过，按规格降级（仅前台可交互 / Wi-Fi 广播一次性登录 / 路由侧兜底），
  不硬堆后台技巧。
- **v1 只承诺 v4**；v6 补绑默认关，等 A5 + V1/V3 多设备实测再开。

## 1. 为什么不是"把 Go 二进制搬过去"

Android 10+ 禁止应用执行私有目录里的可执行文件：

> "Untrusted apps that target Android 10 cannot invoke `execve()` directly on
> files within the app's home directory." — Android 10 behavior changes

所以独立 Go 二进制/Termux 形态不是产品方案。引擎必须以 `.so` 进 APK，
由 Kotlin 进程内调用。三条可行路线：

| 路线 | 复用 | 风险 | 结论 |
|---|---|---|---|
| **A. gomobile bind → AAR** | 内核全复用，Kotlin 只写壳 | gobind 类型限制、16KB 对齐、AAR 体积 | **v1 选它** |
| B. Kotlin 全量重写 | 只复用协议文档 | 重造状态机/闸门，双实现漂移 | 否 |
| C. `-buildmode=c-shared` + JNI | 内核全复用 | 手写 JNI、NDK 构建链更长 | A 失败时的后备 |
| D. 独立二进制 / Termux | 无 | Android 10+ W^X 直接禁 | 不可行 |

`golang.org/x/mobile` 仓库 2026-10-11 仍有提交（`go.mod: update golang.org/x
dependencies`），gomobile bind 仍在维护；但 Go 1.24/1.25/1.26 发布说明都没提
16 KB 页，**16KB 对齐必须在 M1 用 ELF 检查实测**（见 §7），不能假设。

## 2. 引擎复用边界

| 层 | 现状 | 安卓处理 |
|---|---|---|
| `internal/protocol` | ac_portal 族登录/握手/词表 | **原样复用** |
| `internal/dialect` | 伪 JSON 应答解析 | **原样复用** |
| `internal/keeper` | 状态机 + 风暴三闸 + 四态 | **原样复用**；Tick 由前台服务驱动 |
| `internal/netstack` | 绑源、校园 DNS、钉 IP、TLS | **复用**；源地址改为 Kotlin 注入（见 §3.4） |
| `internal/api` | 127.0.0.1 + token，八端点 | **复用**；WebView 吃同一契约 |
| `cmd/cnalw/panel.html` | 单文件面板 | **复用**；已有 viewport + max-width 480 |
| `internal/app` | 组合根 | 复用，但需 4 个注入缝（§3） |
| `internal/store` | 仅 Windows 凭据管理器 | 安卓不实现：Kotlin Keystore 保管后注入（§4.3） |
| `cmd/cnalw` | 托盘/WebView2/菜单 | 替换为 Kotlin Activity + 通知 |
| `cmd/cnald` | 无头 tick 循环 | 替换为前台服务 |
| 测试 | host 上 `go test ./...` | 继续在 host 跑；AAR 只承载运行时代码 |

**面板契约不变**：`/state`、`/login`、`/panel`、`/config`、`/flux`、
`/accounts`、`/rebindv6`、`/logs` 全部沿用；"全功能对齐"的判据是契约字段与
动作集合一致，不是像素一致。

## 3. 需要新增/改动的接缝

### 3.1 桥接层

`internal/api.Server` 有函数字段（`Snapshot func() Snapshot` 等），gobind
不适合直出这种结构。新增 `mobile/` 薄封装包，只暴露 JSON 字符串/错误：

```go
func Start(configPath string) error
func Stop()
func StateJSON() string          // /state 同构
func Login() error
func RebindV6() error
func FluxJSON() (string, error)
func LogsJSON() string
func SetCredentials(user, pass string) error
func SwitchAccount(user string) error
```

Kotlin 只依赖这 9 个方法；内部仍走 `app.Compose` 与同一套 API 契约。
`Start` 内部起与 `cmd/cnalw` 相同的 tick goroutine（`Daemon.Tick()` +
`cfg.Interval` 睡眠）；前台服务只负责让进程活着，不参与状态机。

**2026-11-10 进展**：桥已落地在 `mobile/`（host 可验证部分，见
`docs/m1-host-verifiable.md`）。除上述 9 个方法外另有 3 个 Start 前置注入
（`SetDataDir` / `SetPhysicalSources` / `SetPanelPage`）与 `SetCredentials`；
`SetPanelPage` 是因为面板 HTML 目前是 `cmd/cnalw` 的私有 embed，AAR 编译
单元拿不到，由 Kotlin 从 assets 读出后注入。**AAR 尚未产出**（NDK/gomobile
未装），真门户联调仍待真机。

### 3.2 凭据接缝

`app.Compose` 现在直接调 `store` 包级函数（`app.go:44/311/336/452`）。
改成注入：

```go
type CredentialSource interface {
    Load(user string) (string, error)
    Save(user, pass string) error
}
```

- Windows：包装现有 `store`，行为零变化。
- 安卓：Kotlin 实现，Keystore 解密后把明文交给 Go（只在内存里，不落盘）。

### 3.3 路径接缝

`app.LogFilePath()` 与 `InstanceLockPath()` 写死 `%LOCALAPPDATA%`/`UserConfigDir`
（`tail.go:13`、`instance_windows.go:31`）。安卓要传 `context.filesDir`：
新增 `SetDataDir(dir string)`（Compose 前调用），日志/配置/锁都从它拼。
单实例锁在安卓是 no-op：Activity 与 Service 同进程，系统不允许同包双进程。
`Compose` 里的 `fmt.Printf`（v4/v6 源、重发现）在安卓 stdout 不可见，
改为注入 logger 写 `/logs` 同一文件；M1 顺手做。

### 3.4 物理源接缝

`Compose` 现在直接调 `netstack.DiscoverSource` / `PhysicalSourceV6`
（`app.go:52-53`）。安卓由 Kotlin 从 `LinkProperties` 取 Wi-Fi 的 v4/v6 地址
与接口名传进来：

- v4：`wlan0` 的 IPv4；v6：`wlan0` 的全局 GUA（没有就 nil）。
- 为什么不让 Go 自己枚举：安卓上 `rmnet`（蜂窝）也可能有全局 v6，
  `PhysicalSourceV6` 的"第一个非虚拟网卡"会选错栈。
- `netstack` 的绑源与钉 IP 保留：作为"进程绑网之外的第二道保险"。
- v6 源必须可热更新：安卓临时地址会轮换，像 `currentV4` 一样加
  `currentV6` + `SetV6Source`，`handshakeOnce` 与 `Snapshot.V6Src`
  （`app.go:272`）都读共享引用；否则轮换后握手恒失败，A5 的"轮换后重绑
  ≤3 次收敛"无法实现。
- `Snapshot.V6Src` 不再直接调 `netstack.PhysicalSourceV6()`，改读注入值。

## 4. Kotlin 壳的职责

### 4.1 后台存活（方案的核心风险）

**前台服务类型选 `specialUse`**（Android 14+ 必须声明类型）：

```xml
<service android:name=".KeeperService"
         android:foregroundServiceType="specialUse"
         android:exported="false">
  <property android:name="android.app.PROPERTY_SPECIAL_USE_FGS_SUBTYPE"
            android:value="campus network session keeper"/>
</service>
```

加 `FOREGROUND_SERVICE_SPECIAL_USE` 权限；Play 上架时 subtype 文案会被审核，
侧载不受此限。

选它的官方依据：

- Android 15 的 6 小时/24 小时超时**只适用于 `dataSync` 与 `mediaProcessing`**，
  `specialUse` 不在名单里（FGS timeouts 文档）。
- Android 15 的 `BOOT_COMPLETED` 禁启名单是 `dataSync`/`camera`/`mediaPlayback`/
  `phoneCall`/`mediaProjection`/`microphone`，**`specialUse` 不在名单里**；
  且通用豁免明确包含 `ACTION_BOOT_COMPLETED`。
- WorkManager 周期任务最小 15 分钟（`PeriodicWorkRequest` 文档），做不了
  30-60 秒守护；它只适合当"看门狗"：每 15 分钟检查服务是否活着，死了拉起。

存活组合拳（M2 实现）：

1. `startForegroundService` + `startForeground(id, notification, type)`；
2. `START_STICKY` + `onTaskRemoved` 不自杀；
3. `BOOT_COMPLETED` receiver 启动服务；
4. WorkManager 15 分钟看门狗；
5. `REQUEST_IGNORE_BATTERY_OPTIMIZATIONS` 引导页 + 国产 ROM 自启动白名单引导；
6. 通知常驻：状态、上次登录时间、动作（立即重连 / 暂停自动重连 / 打开面板）。
   服务每 5s 读一次 `StateJSON()`，状态变了才更新通知（避免通知栏刷屏）。

`POST_NOTIFICATIONS` 是 Android 13+ 运行时权限；即使用户拒绝，前台服务仍能启动，
但通知只在任务管理器可见（官方文档）。所以首次启动必须引导授权。

### 4.2 网络选择与物理源

Kotlin 侧：

1. `ConnectivityManager.getAllNetworks()` / `registerDefaultNetworkCallback`；
2. 选 `TRANSPORT_WIFI` 且不含 `NET_CAPABILITY_VPN` 的 `Network`；
3. `bindProcessToNetwork(network)`；
4. 从 `LinkProperties` 取 v4/v6 地址注入 Go（§3.4）；
5. 网络断开回调：清绑定、通知 Go 进入 `portal_down`、等下一个 Wi-Fi。

官方文档对 `bindProcessToNetwork` 的承诺是：

> "All Sockets created in the future ... will be bound to `network`."
> "All host name resolutions will be limited to `network` as well."

**但文档没有明确说 native socket 是否受影响**。Go 的 socket 由 native 层创建，
所以 A4 必须真机验证：FlClash 同时开着时，绑 Wi-Fi 后门户/握手看到的出口
是否仍是校园网。A4 不过 → 安卓探测整体不可信，按规格降级，不硬上。

另一个官方警告：绑定的 network 断开后，所有已建 socket 会失效、域名解析会失败。
所以 Kotlin 必须在网络变化时重新绑定并让 Go 重连，不能一绑了之。

### 4.3 凭据接缝

`androidx.security:security-crypto` 1.1.0 已明确"Deprecated all APIs in favour of
existing platform APIs and direct use of Android Keystore"。所以：

- Keystore 生成不可导出的 AES-256-GCM 密钥（不要求用户生物认证，守护要无人值守）；
- 密码密文存 app 私有 DataStore/SharedPreferences；
- 只有 Kotlin 能解密；Go 通过 `CredentialSource` 拿明文（只在内存）；
- 账号切换 = Kotlin 解密后调 `mobile.SwitchAccount`；
- 日志/崩溃报告永不打印密码或 token。

### 4.4 UI 与回路契约

- `MainActivity` 内嵌 `WebView`，加载
  `http://127.0.0.1:<port>/panel?token=<token>`；`javaScriptEnabled`、
  `domStorageEnabled`、禁文件访问。
- targetSdk 28+ 默认禁 cleartext；需在 `network-security-config` 为
  `127.0.0.1` 放行，或用 `usesCleartextTraffic`（范围更大，不推荐）。
  **M1 第一天验证**，这是最容易卡住的一步。
- 绑定 Wi-Fi 后 loopback 是否仍可访问，M1 第一天一起验证（理论上 loopback
  不受默认网络影响，但 `bindProcessToNetwork` 的文档没给这个保证）。
- 面板已适配手机（`<meta name="viewport">`、`max-width: 480px`），
  只需在 Activity 处理系统栏 insets（Android 15 edge-to-edge 强制）。
- 现有按钮高度 28-32px：桌面鼠标可用，触屏不达标。M1 在 WebView 里把
  可点面提到 ≥44px（intentional-design 绝对标准），只放大触控面，不改视觉密度。
- 日常入口是通知；面板负责设置、日志、流量、诊断。

## 5. 范围、门槛与多设备风险

### 5.1 A1-A5 准入门槛（先测再写产品）

| # | 测什么 | 通过线 | 决定什么 |
|---|---|---|---|
| A1 | 息屏 30 分钟存活 | 漏拍 0；模拟踢线后 ≤5s 恢复 | 前台服务形态是否成立 |
| A2 | MIUI/EMUI 过夜 8h | 门户侧登录日志计数：每次被踢都恢复 | OEM 白名单引导是否够 |
| A3 | 省电 × Wi-Fi 空闲超时 | 8h 不掉；心跳功耗 >3%/h 则降级 | 心跳周期/产品形态 |
| A4 | 绑 Wi-Fi 且 FlClash 在跑 | 门户/握手侧出口 100% 正确 | **不过则安卓探测不可信** |
| A5 | 校园 SSID 下 v6 GUA | 有 GUA 且轮换后 ≤3 次重绑收敛 | v6 是否进 v1 |

M0 用**纯 Kotlin 测量 APK**（不接 Go）跑 A1/A3/A4/A5：前台服务 + 绑网 +
30 秒 HTTP 探针 + 8 小时间隔日志；A2 需要 MIUI/EMUI 真机过夜。
A4 不过就停在 M0 重新定形态，不进 M1。
逐项步骤、记录模板与判定矩阵见 `docs/android-a1-a5-testplan.md`
（2026-11-08：计划就绪，真机未执行）。

### 5.2 v1 功能范围

- v4 登录、守护、手动重连、自动重连开关、流量、日志、设置、账号切换；
- 通知动作：立即重连 / 暂停 / 打开面板；
- v6：默认关；A5 过 + V1/V3 多设备实测通过后再开补绑；
- 面板的"补绑 IPv6"按钮保留，但 `dual_stack=false` 时后端拒绝（现有语义）。

### 5.3 多设备风险（必须先看）

账号有 4 个关联地址上限（规格 §11.1）。手机开双栈、PC 也开双栈，可能吃掉
4 个名额并互相踢线。**v1 默认手机只 v4、不补绑**；如果检测到同账号 PC 守护
也在线，面板提示"多设备可能互踢"。V1-V5 实测前不承诺双栈共存的稳定性。

### 5.4 不做

- root、无障碍自动化、VPN/代理规避、无通知的后台常驻；
- v1 不上 Google Play（侧载 APK；Play 还要处理 16KB、FGS 类型审核、政策声明）。

## 6. 里程碑

| 阶段 | 内容 | 验收 | 预估 |
|---|---|---|---|
| M0 | 测量 APK（纯 Kotlin） | A1/A3/A4/A5 数据；A4 不过即停 | 2-3 天 |
| M1 | `mobile/` 桥 + `app` 四缝 + AAR；前台服务驱动 Tick；WebView 面板；Keystore | 真门户登录、状态迁移、杀进程重启恢复 | 3-5 天（**host 全部做完 2026-11-10：四缝 + 桥 + AAR + Kotlin 壳 + WebView/Keystore/通知 + 单测 + 可侧载 APK；真机验收统一排最后，见 `docs/m1-host-verifiable.md` §5–6**） |
| M2 | BOOT_COMPLETED、电池白名单、OEM 引导、WorkManager 看门狗、过夜 A2 | 8h 不掉；被踢自动恢复 | 3-5 天 |
| M3 | 签名、ABI 拆分、16KB 检查、体积/内存、adb 验收脚本 | APK ≤20MB；服务增量 ≤40MB；脚本全绿 | 2-3 天 |

总计约 2-3 周（一人）；M0 失败则缩短为"仅前台/一次性登录"形态，不进入 M1-M3。

## 7. 构建、体积与验收

- 工具链：JDK 17、Android SDK 35、NDK r28+、AGP 8.5.1+、gomobile、Go 1.27。
  **2026-11-09 已装**：Temurin JDK 17.0.21+9 → `D:\Android\jdk`；
  Android cmdline-tools + platform-tools(adb 37.0.1) + platforms;android-35
  + build-tools 35.0.0 + emulator 37.2.13 + system-images;android-35;default;x86_64
  → `D:\Android\sdk`（licenses 已接受）；Gradle 8.9 便携解压
  → `D:\Android\gradle-8.9`（**winget 源里没有 `Gradle.Gradle`**，实测
  `找不到与输入条件匹配的程序包`）；AVD `medium_phone` 已建，WHPX 加速
  `emulator -accel-check` = usable。用户级环境变量 `JAVA_HOME` /
  `ANDROID_HOME` / `ANDROID_SDK_ROOT` / `GRADLE_HOME` 与 PATH 已设。
  **2026-11-10 已补齐**：NDK `28.2.13676358`（r28，16KB 默认对齐）、
  `gomobile`（go.mod 记 `tool golang.org/x/mobile/cmd/gobind`）；
  `gomobile bind -target=android/arm64 -androidapi 29 -javapkg dev.cnal`
  已产出 `mobile.aar`，Kotlin 壳接线完成。详见 `docs/m1-host-verifiable.md` §5。
- **M0 测量壳已落地**：独立仓库 `../go-cnal-android`（纯 Kotlin，不接 Go 内核）。
  模拟器实测：前台服务 `specialUse` 被接受（`types=0x40000000`）、绑 Wi-Fi 成功、
  30s 节拍连续 12 拍误差 ≤3ms、息屏 130s 与深 Doze 下继续拍；20 个单测。
  **实测记录与"模拟器证明不了什么"见 `../go-cnal-android/docs/m0-findings.md`**。
  M0 同时提前引爆 M1 的雷：`targetSdk≥28` 默认禁明文 HTTP，锚点/面板这类
  `http://` 目标会被系统拦掉，必须走 `network_security_config` 白名单
  （风险 5 的实证；M1 面板的 `127.0.0.1` 也要加）。
- `gomobile bind -target=android -androidapi 29 -o mobile.aar ./mobile`；
  v1 只出 `arm64-v8a`，体积不够再拆 ABI。
- 16KB：NDK r28+ 默认 16KB 对齐；构建后用
  `zipalign -c -P 16 -v 4 app.apk` 与 `llvm-objdump -p libgojni.so | grep LOAD`
  验证（`align 2**14`）。Play 对 targetSdk 35+ 的 16KB 硬截止是 **2027-02-01**；
  侧载不受 Play 限制，但 16KB 设备已经存在，仍要一次做对。
- 体积预算：APK ≤20MB（规格 §10）；`-ldflags="-s -w"`；服务常驻增量 ≤40MB。
- 验收脚本 `scripts/android_verify.py`（adb）：安装、授权通知、启动服务、
  `dumpsys deviceidle` 前后状态、force-stop 后重启、面板截图、logcat 无崩溃。
  与 Windows 的 `release_verify.py` 同思路：脚本判绿才算交差。

## 8. 风险清单

| # | 风险 | 影响 | 缓解 |
|---|---|---|---|
| 1 | `bindProcessToNetwork` 对 native socket 的效力文档未明说 | 整个探测可能走 VPN 出口 | A4 真机先测；不过则降级/要求关 VPN |
| 2 | 国产 ROM 杀进程 | 夜间掉线不恢复 | A2；自启动白名单引导；WorkManager 看门狗 |
| 3 | 4 地址配额互踢 | 手机把 PC 踢下线 | 手机默认只 v4；V1/V3 实测；面板提示 |
| 4 | gomobile/16KB 工具链 | 装不上/上架被拒 | NDK r28+；ELF 对齐检查；后备 c-shared |
| 5 | WebView 禁 cleartext | 面板白屏 | network-security-config 放行 127.0.0.1 |
| 6 | Keystore 密钥不可导出 | Go 无法自行解密 | Kotlin 做凭据库，接口注入明文 |
| 7 | Doze/Wi-Fi 空闲超时 | 心跳断、会话掉 | A1/A3；必要时提高心跳频率（功耗换算） |
| 8 | Android 15 edge-to-edge | 面板被系统栏遮挡 | Activity 处理 insets |
| 9 | Android 16/17 新限制 | 未来升级破坏 | v1 targetSdk 35；发布前复查 behavior changes |

## 9. 默认决策（需要改再谈）

- 分发：**侧载 APK**（项目现有 Windows 分发也是 zip + 脚本，不进商店）。
- 手机与 PC 同账号：**手机默认只 v4、不补绑**，先保 PC 不被踢。
- v1 不做 v6 卖点；A5 过线后再开。

## 10. 参考

- 设计规格 §11（A1-A5、多设备 V1-V5、降级形态）：
  `../campus-net-autologin/docs/superpowers/specs/2026-11-06-dualstack-and-go-port-design.md`
- FGS 超时（dataSync/mediaProcessing 6h）：
  https://developer.android.com/develop/background-work/services/fgs/timeout
- FGS 类型（specialUse 声明与属性）：
  https://developer.android.com/develop/background-work/services/fgs/service-types
- Android 15 BOOT_COMPLETED 限制（specialUse 不在名单）：
  https://developer.android.com/about/versions/15/behavior-changes-15#fgs-boot-completed
- 后台启动豁免（含 BOOT_COMPLETED）：
  https://developer.android.com/develop/background-work/services/fgs/restrictions-bg-start
- `bindProcessToNetwork`：
  https://developer.android.com/reference/android/net/ConnectivityManager#bindProcessToNetwork(android.net.Network)
- WorkManager 最小周期 15 分钟：
  https://developer.android.com/reference/androidx/work/PeriodicWorkRequest
- 通知运行时权限：
  https://developer.android.com/develop/ui/views/notifications/notification-permission
- 16KB 页与 Play 截止 2027-02-01：
  https://developer.android.com/guide/practices/page-sizes
- Android 10 W^X：
  https://developer.android.com/about/versions/10/behavior-changes-10#execution-restrictions
- security-crypto 废弃、直接用 Keystore：
  https://developer.android.com/jetpack/androidx/releases/security
- gomobile 仓库（2026-10 仍活跃）：
  https://github.com/golang/mobile
