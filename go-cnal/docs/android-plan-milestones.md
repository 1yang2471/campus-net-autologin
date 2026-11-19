# 安卓版里程碑、构建与风险（android-plan.md 续篇）

> 本文是 [android-plan.md](android-plan.md) 的第 6–10 节。拆分理由：单文件守住
> 20000 字节提交闸（tidy §3）。设计规格（§0–§5）在前篇。

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
