# Windows 三刀修复经验向安卓端移植（2026-11-21）

## 背景：错位状态

Windows 端 29fee2f（2026-11-21）落地假在线三刀修复 + 通道登录恒拒 +
产品五项 + StoreFluxView P0 数据竞争修复。安卓仓库当时处于**前端新、
内核旧**的错位状态：

- `app/src/main/assets/panel.html` 已随 29fee2f / 1bf8b65 同步（+15/+8 行，
  含 LastAction 与通道登录事件日志字段）；
- `app/libs/mobile.aar` 停在 2026-11-19 1469aa0 的 bind——落后三笔内核
  修复（7fefe08 / 1bf8b65 / 29fee2f）。

此状态直接 `assembleDebug` 会打出错位包：面板新字段读不到（旧内核
stateJSON 无 LastAction），且旧内核带着 StoreFluxView P0 数据竞争
（WebView 面板查 /flux 同路径触发，可致进程崩溃）。

时机判断：A1–A5 真机验收尚未开跑（`m1-host-verifiable.md` §头部），
无作废数据——此时重 bind，验收测到的才是含三刀修复的内核。

## 经验映射表

| 类别 | 内容 | 载体 | 状态 |
|---|---|---|---|
| 自动继承 | 刀 1：probeAnchor 先裸 IP 后 Host 变体、劫持即 break、err 即 break | 重 bind 进 AAR | ✅ 2026-11-21 重 bind |
| 自动继承 | 刀 2：flux 熔断不连坐 SessionAlive；L2 持续受限告警文案 | 同上 | ✅ |
| 自动继承 | 刀 3：「已在线」哨兵三分支（复核活/死/未知） | 同上 | ✅ |
| 自动继承 | StoreFluxView P0 数据竞争 → 原子交接槽 | 同上 | ✅ |
| 自动继承 | 通道登录恒拒修复 + 动作时间线、门户休眠观察、切账号流量修复 | 同上 | ✅ |
| 壳侧已就绪 | 面板 LastAction / 通道登录事件日志字段 | assets/panel.html（29fee2f 已同步） | ✅ 无需改动 |
| 不适用 | NCSI 弹窗治理三层 | — | Android 的 captive portal 检测是系统级另一套，不由本 app 管 |
| 不适用 | 多网卡双通道自愈 | — | Android 单宿主网（bindProcessToNetwork），无 Windows 双网卡形态 |
| M0 边界 | 见下节 | 文档化 | 不改测量口径 |

## M0 锚点白名单边界（新发现，不改代码的理由）

`KeeperService.ANCHOR = "http://connect.rom.miui.com/generate_204"` 正是
Windows 端刀 1 的白名单陷阱本体：参考校把该域名白名单放行（go-cnal
`internal/app/detect.go` 2026-11-11 实测注释），未认证时请求不劫持、
返回 404。

两端的受害形态不同，关键在判定表口径：

- **Windows 端（受害）**：旧 probeAnchor 判定表把非重定向非指纹命中一律
  当在线 → 404 假绿 → 守护不补登（已修）。
- **M0（碰巧读对）**：`ProbeLogic.kt` 判定表 `else -> OFFLINE`——404 归
  掉线。未认证时段白名单返 404，M0 记 OFFLINE，在体验语义上恰好正确。

**剩余风险**：校园对白名单域名改返 200 空页（无门户指纹）时，M0 判
ONLINE 假绿，A1/A3 在线率读数被污染。本校未踩中此形态，但**异校跑
A1–A5 前应先抓一次未认证读数核对**（未认证时 M0 应记 OFFLINE 而非
ONLINE）。

不改测量口径的理由：A1–A5 判据依赖既定日志形态（`ProbeLog.line` 定宽
四字段），改锚点结构会引入测量口径变化风险；且参考校 404 形态下当前
判定读对。此边界记录于此，供验收时对照。

## 重 bind 实录（2026-11-21 13:42）

AGENTS.md 原记录的 bind 命令缺 `-androidapi`——照原命令跑会报
`unsupported API version 16 (not in 21..35)`（NDK 28 只支持 21+，
gomobile 默认 16）。完整命令出处是 `app/build.gradle.kts:76` 注释，
已回修 AGENTS.md：

```bash
# Git Bash 下（ANDROID_NDK_HOME 环境变量默认未设，需临时带上）
export ANDROID_NDK_HOME="D:\Android\sdk\ndk\28.2.13676358"
cd go-cnal
gomobile bind -target=android/arm64 -androidapi 29 -javapkg dev.cnal \
  -o ../go-cnal-android/app/libs/mobile.aar ./mobile
```

闸门与验证：

| 项 | 结果 |
|---|---|
| AAR 体积 | 5995469 → 6023268 字节（+27KB 代码增量） |
| 安卓单测 | `:app:testDebugUnitTest --rerun` 91 tests，failures=0，errors=0 |
| 构建 | `:app:assembleDebug` 成功，APK 含 libgojni.so（15.6MB）+ 新 panel.html |
| 特征验证 | APK 内核二进制四中四：「复核不可得」「休眠观察」「出口受限持续较久」「通道登录」 |
| 导出面 | `mobile.go` 无改动，Kotlin 壳零改动（ABI 未变，gradle 判定无需重编译） |

## 下次 A1–A5 真机验收注意

1. M1 验收即测**新内核**——三刀行为语义在真机上有新表现：哨兵复核
   （判死→重登答已在线→当场复核 IP 会话）、门户休眠观察（不可达 10 分钟
   进休眠文案）、L2 持续受限告警。验收记录表按新语义填。
2. M0 在线率读数的白名单效度边界（上节）——参考校 404 形态下读数有效；
   异校先核对未认证读数。
