# M1 可验证部分：host 侧地基（2026-11-10）

> 对应 `docs/android-plan.md` §3（四个注入缝）与 §3.1（mobile/ 桥）。
> **这一轮只做不依赖真机的部分**；A1–A5 仍未判定（`android-a1-a5-testplan.md`
> 状态：未执行），按计划门槛不宣称 M1 完成。

## 0. 为什么要先做这一层

`GOOS=android GOARCH=arm64 go build ./internal/...` 在改动前直接失败：

```
package cnal/internal/app
    imports cnal/internal/store: build constraints exclude all Go files
```

`internal/store` 全是 `//go:build windows`（凭据管理器直调 advapi32），而
`internal/app` 无条件 import 它。所以「切断 app 对 store 的直接依赖」是 AAR
能编译的硬前提，不是可选优化。

## 1. 做了什么

### 1.1 四个注入缝（`internal/app/inject*.go`）

| 缝 | 入口 | Windows 默认（行为零变化） | 安卓注入 |
|---|---|---|---|
| 凭据 | `SetCredentialSource(CredentialSource)` | `inject_windows.go` 包装 store 的 Load/LoadLegacy/Save | Kotlin Keystore 解密后 `SetCredentials`，明文只住内存 |
| 路径 | `SetDataDir(dir)` | `%LOCALAPPDATA%/campus-net-autologin` | `context.filesDir`，config/log/api.json/lock 一起跟着走 |
| 物理源 | `SetPhysicalSources(PhysicalSources{V4,V6})` | `netstack.DiscoverSource` / `PhysicalSourceV6` | `wlan0` 的 v4 + 全局 GUA v6；V6 轮换后重新注入即热更新 |
| 打印 | `SetSourcePrinter(SourcePrinter)` | `fmt.Printf`（stdout） | 收进内存日志，`LogsJSON` 带出 |

改动点：
- `app.go` / `setup.go` / `compose_api.go` 去掉 `store` 直接调用，改走
  `credentialSource()`；`CredService` 别名撤回 store 包内部。
- `tail.go` 的 `DataDir()` 收成单拼接点 `dataDir()`，四个路径函数
  （config/log/api.json/lock）全部经它。
- `compose_portal.go` 新增 `currentV6` 共享引用，`Snapshot.V6Src`、
  `handshakeOnce`、`onlineV6`/`logoutV6` 都读注入值而非重新枚举网卡；
  两处 `fmt.Printf` 改 `printSource`。
- `compose_daemon.go` 的 `ProbeV6` 改用 `currentV6()`。

### 1.2 `mobile/` 桥接包（`go-cnal/mobile/mobile.go`）

gobind 只暴露 JSON 字符串 / error / 基础类型，Kotlin 不碰 `app.App`。

- 9 个契约方法：`Start(configPath) (panelURL,error)`、`Stop()`、
  `StateJSON()`、`Login()`、`RebindV6()`、`FluxJSON()`、`LogsJSON()`、
  `SetCredentials(user,pass)`、`SwitchAccount(user)`。
- 3 个 Start 前置注入：`SetDataDir`（同时装配凭据源与内存打印器）、
  `SetPhysicalSources(v4,v6)`、`SetPanelPage(html)`；`SetCredentials` 另调。
- `Start` 内部走与 `cmd/cnald` 相同的 `app.Compose` + `Daemon.Tick` +
  `Interval` 睡眠；前台服务只负责让进程活着。
- 回路 API 仍只绑 `127.0.0.1`，token 每次启动随机；端点文件写进注入目录，
  与 Windows `cnal status` 同一份 `RuntimeInfo` 契约。

### 1.3 Android 侧白名单（`go-cnal-android`）

`network_security_config.xml` 加 `<domain>127.0.0.1</domain>` 到
`cleartextTrafficPermitted="true"` 的 domain-config。M0 实测
（`go-cnal-android/docs/m0-findings.md` §2）已证明这是 M1 第一天的白屏雷。

## 2. 测试门（2026-11-10 同轮实测）

- `go test ./... -count=2`：全绿（13 包；含新增 `internal/app/inject_test.go`
  5 个、`mobile/mobile_test.go` 4 个）。`-count=2` 确认注入缝的全局状态无泄漏。
- `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./internal/... ./mobile/...`：通过。
- `go vet ./...`：通过。
- Android `:app:testDebugUnitTest --rerun-tasks`：**22 个用例全绿**
  （`CleartextPolicyTest` 2 个真实执行，报告 XML 已核对）；
  `:app:assembleDebug`：BUILD SUCCESSFUL，`app-debug.apk` 5,365,665 B。
- 变异检查（Go）：把 `currentV6` 改回「捕获启动快照」，
  `TestV6RotationIsVisibleToSnapshot` 报
  `轮换后面板仍显示旧地址 "2001:db8::1"，期望 "2001:db8::2"`；改回后通过。
- 变异检查（Android）：删掉白名单里的 `127.0.0.1` 域，
  `CleartextPolicyTest.loopbackPanelIsExplicitlyAllowed` 报
  `AssertionError at CleartextPolicyTest.kt:39`；改回后通过。

## 3. 明确没做（等真机 / 等 M3）

| 项 | 为什么没做 |
|---|---|
| 真门户登录、状态迁移、杀进程重启恢复 | A1–A5 门槛未过；模拟器不参与判定，统一排到最后 |
| A4 绑网对 native socket 的实效 | 必须真机 + 门户侧出口 IP（`android-a1-a5-testplan.md` §4） |
| APK ≤20MB / `-ldflags="-s -w"` / ABI 拆分 / 签名 | M3；当前 debug APK 20.8MB，`stripDebugDebugSymbols` 无法 strip `libgojni.so`（15.3MB） |
| `api.Server` 的优雅关闭 | 既有 API 无 Shutdown；`Stop` 后 `running=nil` 让所有方法报「守护未启动」，回路监听 goroutine 随进程退出（AAR 内通常不 Stop） |
| BOOT_COMPLETED / WorkManager 看门狗 / OEM 白名单引导 | M2 |
| Keystore 密钥的机型兼容性实测 | 需真机；host 只跑通编译与纯逻辑单测 |

## 4. 面板无障碍（2026-11-10 追加，host 可验证）

按 intentional-design Review mode 审计 `cmd/cnalw/panel.css`（设计系统唯一
出处 `DESIGN.md`），修了三处绝对标准违规，全部先 RED 后 GREEN：

| 元素 | 违规 | 修复 |
|---|---|---|
| `details summary` | 粗指针下 `min-height: 0px`，高级设置/登录表单在手机上点不开 | 纳入 `@media (pointer: coarse)` 44px 规则 |
| `button:disabled` | hover 把边框点亮成可点暗示（`button:hover` 未排除 disabled） | `button:hover:not(:disabled)` + `cursor: not-allowed` |
| `.free-row .free-del` | 375px 下两字「删除」被挤成竖排 | time 输入可伸缩、按钮 `nowrap` + `flex: none` |

验证脚本 `scripts/panel_a11y_verify.py`（Python Playwright + 本机 Chrome，
375px + `has_touch` 粗指针、真实本地 HTTP 伺服面板页）：**10 项全绿**——
34 个交互控件 ≥44px、删除标签单行、禁用 hover 无暗示、长串/空态无横向
溢出、键盘 Tab 焦点可见、reduced-motion 无过渡、375/768/1280 三档重组、
控制台无 error。截图 `docs/screenshots/panel-a11y-{settings,flow}.png`。
Go 侧 `TestPanelCSSHasTouchTargets` 与浏览器脚本两处都做了变异检查。

## 5. M1 完整 app（2026-11-10 追加，host 全部做完）

工具链与 AAR：
- 装 NDK `28.2.13676358`（r28 稳定版，默认 16KB 对齐）+ `gomobile`
  （`go get -tool golang.org/x/mobile/cmd/gobind` 记录到 go.mod）。
- `gomobile bind -target=android/arm64 -androidapi 29 -javapkg dev.cnal
  -o app/libs/mobile.aar ./mobile` → AAR 5.9MB，生成
  `dev.cnal.mobile.Mobile` 的 12 个 native 方法。
- **16KB 对齐实测通过**：AAR 与最终 APK 内的 `libgojni.so` 四个 LOAD 段
  全部 `align 2**14`（M3 判据提前满足）。

Kotlin 壳（`go-cnal-android`）：
- `CnalKernel`：唯一装配点。`SetDataDir(filesDir)` → Keystore(AES-256-GCM)
  解密 → `SetCredentials` → `SetPanelPage(assets/panel.html)` →
  `SetPhysicalSources(v4, GUA v6)` → `Mobile.start(config.ini)`。
  门户/账号/密码在界面输入，config.ini 由 `saveServer` 生成（与 Go 侧
  WriteStarterConfig 同形）。
- `PanelActivity`：WebView 容器，只放行 `http://127.0.0.1:<port>`，
  `javaScriptEnabled`/`domStorageEnabled`、禁文件访问、处理 insets。
- `KeeperService` 新增 `ACTION_START_M1`：绑网 → 起内核 → 5s 轮询
  `StateJSON()` 刷通知（状态不变不刷屏）；M0 的 30s 测量循环原样保留给 A1–A5。
- `PhysicalSource`：v6 只取全局 GUA（2000::/3），拒绝 `fe80::`（M0 实测
  模拟器只有链路本地）；`LoopbackOnlyClient` 守住 WebView host 白名单。
- 面板源料仍只有 `cmd/cnalw` 一份，由 `TestExportPanelPageForBrowser`
  导出到 Android `assets/panel.html`（32KB 自包含页）。

host 验证门（六道全绿）：`go test ./...`、`go vet ./...`、
`GOOS=android go build ./internal/... ./mobile/...`、真 Chrome 面板 a11y
10 项、Android `:app:testDebugUnitTest` 32 用例、`:app:assembleDebug`。
产物 `app/build/outputs/apk/debug/app-debug.apk` 20.8MB（debug 签名，
只侧载）。

## 6. 真机验收（统一排最后，条件齐备才启动）

**前置**：APK 侧载到手机（USB 调试）、连校园 Wi-Fi、在界面填门户地址 +
账号密码并保存 → 点「启动 M1 守护」→ 点「打开面板」。

1. A1–A5 按 `android-a1-a5-testplan.md` 逐项填记录表；A4 不过立即停，
   不进 M1 全量；
2. WebView 面板在真机上复核视觉与交互（本轮 10 项是桌面 Chrome 的
   host 验收，不冒充真机）；
3. 真门户登录、状态迁移、杀进程重启恢复、Keystore 机型兼容；
4. M2（BOOT_COMPLETED/看门狗/OEM 引导）、M3（体积/签名/ABI）。

**装机前注意**：APK 是 debug 签名、只支持 arm64-v8a；`config.ini` 的门户
端点路径是参考校事实，异校可能需要改 `login_path`/`login_form`。
