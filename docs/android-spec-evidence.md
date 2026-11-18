# 安卓方案证据镜像（claimrail repo 证据）

原文：`../campus-net-autologin/docs/superpowers/specs/2026-11-06-dualstack-and-go-port-design.md`
（《双栈与 Go 移植设计规格》，2026-11-06）。

本文件只镜像安卓方案用到的原文段落，供 claimrail 的 `repo` 证据在 go-cnal
仓库内回访；**内容以原文为准**，原文改动时同步改这里。

## 1. 凭据（原文 §4 架构，第 123 行）

> 凭据：Windows Credential Manager / Android Keystore，绝不写入 ini

## 2. UI 外壳（原文 §4 关键决策 1，第 136 行）

> web 面板（系统 WebView）。两份 UI 共吃的仍是 `internal/api` 同一契约，因此"全功能对齐"
> 的判据是**契约字段与动作集合一致**，而不是像素一致。UI 层的实际新增只有一份（web 面板）。

## 3. 网络选择（原文 §4 关键决策 4，第 143 行）

> 4. **Android 的网络选择归 Kotlin**：`bindProcessToNetwork` 是 Java framework API，Go 调不了；
> 由 Kotlin 侧选出校园 `Network` 并绑定，把状态注入引擎，`netstack` 只负责绑源与钉 IP（§11）。

## 4. 引擎形态（原文 §11 硬约束 1，第 354-355 行）

> 1. Android 10+ 禁止执行 app 私有目录里的可执行文件 → "带个 Go 二进制跑" **不成立**。
>    引擎必须经 `gomobile bind` 编成 `.aar`（内含 `.so`）由 Kotlin 进程内调用。

## 5. 后台存活（原文 §11 硬约束 3，第 361 行）

> 3. 后台存活不是"150 行 Kotlin"：前台服务类型（Android 14 必须声明，走 `specialUse`）、
>    API 33 通知运行时权限、`ACTION_IGNORE_BATTERY_OPTIMIZATIONS`、国行 OEM 自启白名单引导 UI、
>    `BOOT_COMPLETED` 在多家 ROM 被阉割——按 **400-600 行**预算。

## 6. v6 范围（原文 §11 A5，第 375 行）

> | A5 | 手机 v6 现实 | 校园 SSID 下是否拿到可绑定 GUA、临时地址轮换频率 | 存在 GUA 且轮换后重绑 ≤3 次收敛；否则**安卓砍掉 v6 卖点只留 v4** |

## 7. 多设备预算（原文 §11.1 会话前提，第 397 行）

> 会话前提：账号在 4 个关联地址上限内多占设备位；手机需对该 SSID 关闭随机 MAC。
