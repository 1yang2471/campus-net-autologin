# cnal-android：安卓守护（M0 测量壳 + M1 Go 内核）

`go-cnal` 的安卓版。M0 测量壳（纯 Kotlin）与 M1 Go 内核（AAR）在同一个
APK 里共存：
- **M0**（`启动服务`）：前台服务存活 + 绑 Wi-Fi + 30s 探针 + 落盘，
  是 A1–A5 真机门槛的测量依据，原样保留。
- **M1**（`启动 M1 守护`）：`libgojni.so` 内嵌 Go 内核，WebView 面板复用
  桌面 `cnalw` 同一份回路契约；凭据进 Android Keystore，物理源从
  `LinkProperties` 注入。

设计与判定标准在 `../go-cnal/docs/android-plan.md`、
`../go-cnal/docs/android-a1-a5-testplan.md`；M1 host 进展与真机验收顺序见
`../go-cnal/docs/m1-host-verifiable.md` §5–6。

M0 实测记录（含模拟器证明不了什么）见 [`docs/m0-findings.md`](docs/m0-findings.md)。

## 装机使用（真机验收时才做）

1. 侧载 `app/build/outputs/apk/debug/app-debug.apk`（debug 签名、仅 arm64）；
2. 界面填「门户地址 / 账号 / 密码」→ `保存配置/凭据`（写 config.ini + Keystore）；
3. `启动 M1 守护` → 通知显示状态 → `打开面板` 进 WebView；
4. A1–A5 仍用 `启动服务`（M0 测量循环），按测试计划逐项填表。

> `panel.html` 由 `go-cnal` 侧 `TestExportPanelPageForBrowser` 从唯一出处
> `panelPage()` 导出，改面板源料后要重跑该测试再构建 APK。

## 工具链（本机实际位置）

| 组件 | 路径 / 版本 |
|---|---|
| JDK | `D:\Android\jdk\jdk-17.0.21+9` |
| Android SDK | `D:\Android\sdk`（platform-tools 37.0.1 / platforms;android-35 / build-tools 35.0.0 / emulator 37.2.13 / system-images;android-35;default;x86_64） |
| Gradle | `D:\Android\gradle-8.9`（便携解压，非 winget：`Gradle.Gradle` 不在 winget 源里） |
| AVD | `medium_phone`（API 35, x86_64；WHPX 加速可用） |

环境变量 `JAVA_HOME` / `ANDROID_HOME` / `ANDROID_SDK_ROOT` / `GRADLE_HOME` 已设到用户级；
`local.properties` 里另有 `sdk.dir`（不入库）。

## 构建 / 测试 / 跑

优先用 wrapper（自带，已实测能从华为镜像引导到 Gradle 8.9）：

```bash
./gradlew.bat :app:testDebugUnitTest     # 单测（JVM，不上模拟器）
./gradlew.bat :app:assembleDebug         # → app/build/outputs/apk/debug/app-debug.apk
```

不用 wrapper 也行（本机全局 Gradle 在 `D:\Android\gradle-8.9`）：

```bash
gradle :app:testDebugUnitTest
gradle :app:assembleDebug
```

**wrapper 的 distributionUrl 指向华为镜像**：`services.gradle.org` 会 302 到
`github.com/gradle/gradle-distributions`，本机实测 504（GitHub releases 在国内不稳）。
换网络环境可改回官方地址，见 `gradle/wrapper/gradle-wrapper.properties` 里的注释。

```bash
# 起模拟器（冷启动）
emulator -avd medium_phone -no-boot-anim -no-snapshot-load -gpu swiftshader_indirect

# 装 + 启动（服务是 exported=false，只能点按钮起，shell 起不来）
adb install -r app/build/outputs/apk/debug/app-debug.apk
adb shell am start -n dev.cnal.m0/.MainActivity
```

读探针日志：`adb shell run-as dev.cnal.m0 cat files/probe.log`

## 代码地图

| 文件 | 职责 |
|---|---|
| `ProbeLogic.kt` | 探针判定（30x / 200+门户指纹 = 劫持）+ 日志行形态 |
| `Cadence.kt` | 30s 节拍与漏拍计数（1.5 倍周期容差） |
| `NetPick.kt` | 选网规则（纯函数：Wi-Fi 且非 VPN，都不合格交白卷） |
| `AnchorPolicy.kt` | 锚点闸（只允许公网 http/https；环回/私网/保留地址一律拒） |
| `ProbeRunner.kt` | 一次探针（不跟随重定向，只读正文前 4KB） |
| `ProbeStore.kt` | 日志落盘（追加 + 尾部读回） |
| `NetBinder.kt` | `bindProcessToNetwork` + LinkProperties 地址 |
| `KeeperService.kt` | 前台服务（`specialUse`），驱动 30s 循环 |
| `MainActivity.kt` | 测量仪表界面（状态 + 按钮 + 日志尾部） |

单测只覆盖纯逻辑那五块；前台服务存活与绑网实效必须真机，模拟器不参与 A1–A5 判定。

## 已知的坑

- **明文 HTTP 默认被禁**（`targetSdk ≥ 28`）。锚点白名单在
  `res/xml/network_security_config.xml`；**M1 的面板 `http://127.0.0.1` 已于
  2026-11-10 加入同一 domain-config**（M0 实测的雷，见
  `docs/m0-findings.md` §2）。`CleartextPolicyTest` 守住两条：回路域必须放行、
  `base-config` 不得全局放行。
- 服务 `exported=false`，`adb shell am start-foreground-service` 会被拒
  （`Requires permission not exported from uid`）——从界面按钮起，或用 `uiautomator dump`
  拿按钮坐标后 `input tap`。
- Git Bash 下给 `adb shell` 传 `/sdcard/...` 会被 MSYS 改写成 Windows 路径，
  需要 `MSYS_NO_PATHCONV=1`。
