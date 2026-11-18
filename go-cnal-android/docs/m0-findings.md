# M0 测量壳：模拟器实测记录

> 2026-11-09。本机（Windows 10.0.26300 / WHPX）Android 模拟器 medium_phone（API 35,
> default/x86_64）。对应 `go-cnal/docs/android-plan.md` §5.1 的 M0 与
> `go-cnal/docs/android-a1-a5-testplan.md`。

## 0. 一句话

**壳跑通了，"活不活"的问题没答。** 前台服务（specialUse）、绑 Wi-Fi、30s 探针、
带时间戳落盘，在模拟器上全部成立；但 A1–A5 的判定要真机，本节数据不构成任何
一条 A 结论（android-a1-a5-testplan.md 开头就把"模拟器不参与判定"写死了）。

## 1. 实测到的（有证据）

| 项 | 结果 | 证据 |
|---|---|---|
| 构建 | `assembleDebug` 通过，APK 5.3 MB | `app/build/outputs/apk/debug/app-debug.apk` |
| 安装/启动 | Success，MainActivity 前台 | `dumpsys activity activities` |
| **前台服务类型被接受** | `isForeground=true foregroundId=1 types=0x40000000` | `0x40000000` = `FOREGROUND_SERVICE_TYPE_SPECIAL_USE`；Android 14+ 不声明类型会直接崩 |
| 绑网 | `Wi-Fi 已绑定（已验证）`，v4 `10.0.2.16/24` | 截图 `docs/m0-02-running.png` |
| v6 | **只有 `fe80::…/64`**（链路本地），无全局 GUA | `dumpsys connectivity` 的 LinkAddresses |
| 30s 节拍 | 连续 12 拍，每拍 **+30.001~003 s**，漏拍 0 | 探针日志（下表） |
| 息屏 130s | 5 拍继续，间隔不变 | 探针日志 |
| **深 Doze 100s** | `mState=IDLE mForceIdle=true` 下 4 拍继续、全 ONLINE | 见 §2 的复现命令 |
| 单测 | 20 个（探针判定/漏拍计数/选网/锚点闸/日志形态） | `gradle :app:testDebugUnitTest` |

探针日志节选（`adb shell run-as dev.cnal.m0 cat files/probe.log`）：

```text
2026-11-09T05:58:52.663Z code=204 ms=326 ONLINE
2026-11-09T05:59:22.664Z code=204 ms=61 ONLINE     ← 息屏开始
2026-11-09T06:01:22.669Z code=204 ms=51 ONLINE     ← 息屏 130s，5 拍全在
2026-11-09T06:03:52.676Z code=204 ms=57 ONLINE     ← 深 Doze 中
2026-11-09T06:04:52.677Z code=204 ms=55 ONLINE
```

**Doze 复现命令**（注意：不先 `enable` 的话 `force-idle` 只会回
`Unable to go deep idle; not enabled`，测了等于没测——本轮第一次就踩了）：

```bash
adb shell input keyevent 26                  # 息屏
adb shell dumpsys deviceidle enable
adb shell dumpsys deviceidle force-idle
adb shell dumpsys deviceidle | grep mState   # 期望 IDLE
adb shell dumpsys deviceidle unforce
```

## 2. 拦路坑：明文 HTTP 被系统拦掉（**M1 的同一颗雷**）

第一轮探针全线 `code=0 ms=0 OFFLINE`，而设备网络是好的（`ping 223.5.5.5` 通、
Wi-Fi `VALIDATED`）。原因不是网络：**`targetSdk ≥ 28` 起系统默认禁止 cleartext
HTTP**，而连通性锚点 `http://connect.rom.miui.com/generate_204` 是明文。

判定不是猜的——把白名单改回 `false` 做变异，日志立刻说出原话：

```text
2026-11-09T05:56:54.890Z code=0 ms=16 OFFLINE err=IOException:
Cleartext HTTP traffic to connect.rom.miui.com not permitted
```

修法：`res/xml/network_security_config.xml` **只放行锚点域名**，不用全局
`android:usesCleartextTraffic="true"`（后者对整个 app 生效，范围过大）。

**为什么这是 M1 的雷**：面板走 `http://127.0.0.1:<port>/panel`，同一条规则会把它
一起拦掉——`go-cnal/docs/android-plan.md` §8 风险 5「WebView 禁 cleartext」说的就是
这件事，本轮拿到了本机实证。M1 的 allowlist 必须同时加 `127.0.0.1`。

顺带确立一条纪律：**探针失败必须带原因**。只记 `OFFLINE` 的话，"没绑上网"和
"明文被拦"在日志里一模一样——这正是上一步查不动的原因。`err=` 尾巴就是为此加的。

## 3. TDD 抓到的两个真缺陷（都在日志格式上）

日志形态是 A1/A3 唯一的算术依据，形态一漂结论就没了。两个缺陷都是单测先红出来的：

1. `OffsetDateTime.toString()` **省略为零的秒/毫秒** → 整点那一拍写成
   `1970-01-01T00:00Z`，列宽随拍变化，按位切日志的算法会在整点崩；
2. 格式化模式 **小写 `xxx` 写 `+00:00`**，只有大写 `XXX` 才按 RFC3339 写 `Z`。

修完钉住：`yyyy-MM-dd'T'HH:mm:ss.SSSXXX`，前四段定宽，失败原因只做 `err=` 尾巴。

## 4. 模拟器**证明不了**什么（不冒充结论）

| A 项 | 模拟器能给 | 为什么不算数 |
|---|---|---|
| A1 息屏 30 分钟存活 | 130s 连续 5 拍 | 时长不够，且无 OEM 后台阉割；判定要真机 + 门户侧日志 |
| A2 过夜 Doze × OEM | 无 | 没有 MIUI/EMUI 真机，AOSP Doze 不等于厂商行为 |
| A3 省电 × 空闲超时 | 无 | 模拟器电量是合成的，`batterystats` 不反映真实心跳功耗 |
| A4 绑网出口正确 | 无 | 本机没有"真 VPN 叠加 + 校园门户"的组合；`bindProcessToNetwork` 对 **native socket** 的效力文档没承诺，只能真机看门户侧出口 IP |
| A5 手机 v6 现实 | 反向证据：只有 `fe80::`，无 GUA | 模拟器网络是 NAT，不反映校园 SSID 的 v6 分配与临时地址轮换 |

**一句话**：本轮交付的是"壳能编译、能装、能活、探针准"，以及一颗已经提前引爆的
M1 地雷；A1–A5 仍然要真机。

## 5. 从这次实测直接长出来的 M1 待办

1. allowlist 加 `127.0.0.1`（面板回路）——否则 M1 第一天就白屏；
2. NDK r28+ 与 `gomobile bind`（AAR 内嵌 Go 内核）；
3. v6 源注入要取**全局 GUA** 并处理轮换（模拟器只有 `fe80::`，正好说明不能照抄
   "第一个 v6 地址"）；
4. 绑网实效（A4）必须真机，且 `bindProcessToNetwork` 官方只承诺 Java socket 与
   域名解析——Go 的 native socket 要实测。
