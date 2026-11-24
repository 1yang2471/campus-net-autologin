# v2.4.1 P0 源失效自愈 — TDD 开工计划（2026-11-24）

> 状态：**已批准（2026-11-24）**——用户拍板接口 A 用显式 `Mobile.RebindSources`
> 导出、实现范围 Go+壳+真机复测全包。按 T1→T2→T3→T4 垂直切片开工。
> 调研输入：两个只读子智能体报告（内核 `chooseRoute` 重选 + 安卓注入缝），
> 结论已交叉验证。根因台账见本文件 §1。
> 测试基线（开工前实测）：`go test ./... -count=1` → 10 包 ok、2 包无测试文件、退出码 0。

## 1. 根因结论（调研实证，file:line）

**核心：注入模式下自愈缝整块没装配，「正在重选」是静态文案。**

| 根因 | 证据 |
|---|---|
| (d) 主因：注入模式 `rd==nil`，`OnEgressInvalid`/`OnPortalDown` 未装配，13 拍零重选 | `compose_portal.go:230-247`（rd 仅 picker!=nil 建）；`compose_daemon.go:53-65`（整个 if p.rd!=nil）；`keeper/egress.go:55-64`（"正在重选"固定文案） |
| (c) 注入 v4 是**冷的**：Compose 时捕获进局部 `v4`，`currentV4()` 只读快照；重注入不改绑客户端；v6 反而是热的 | `compose_portal.go:94-97,133-142`（v4 快照）vs `:145-152`（v6 热读）；四客户端 `:161,171,190,228` |
| (a) 不成立：bind 失败已被识别为 `l1BindInvalid`，只是识别后无自愈动作接 | `keeper/egress.go:85-105,62-69`；`daemon.go:262-264` |
| START_STICKY 陷阱：null intent 重启只起 M0、不起 Go 内核 | `KeeperService.kt:157-171` |

**v4 冷 / v6 热的不对称是这次修复的关键设计点**：两条自愈线（Kotlin 主动重注入、
Go SourceStale 兜底）最终必须汇到同一个「重绑 v4」实现，否则修不好。

## 2. 待确认的接口变更（开工前请你拍板）

### 接口 A：`Mobile.RebindSources()` 显式导出（用户 2026-11-24 拍板）
- 现状（`inject.go:84-89`）：`SetPhysicalSources` 只写全局变量，已 Compose 的客户端不感知。
- 决定：新增显式导出 `Mobile.RebindSources()`（Go 侧 `app.RebindInjectedSources()`），
  重读注入 v4 → 复用 `rd.apply` 那组 `SetSource`（四客户端）+ `pt.invalidate()` 清 DNS 缓存。
  包级注册钩子由 `newPlumbing` 在注入模式注册。
- 效果：Kotlin 调 `setPhysicalSources` 后**再调 `RebindSources`**，v4 才真正改绑。
  AAR 需重新 gobind 构建、Kotlin 侧多一处调用——换取更清晰的语义（显式动作）。
- 被否：SetPhysicalSources 重入即重绑（隐式副作用，用户选显式）。

### 接口 B：Go→Kotlin `SourceStale` 通知缝（D-09）
- 仿 `CredentialSink`（`mobile.go:77-82`）：`type SourceSink interface { SourceStale() }`
  + `Mobile.SetSourceSink(s)`；Kotlin 实现里触发 `bind()+injectSources(bound)`。
- 触发点：`chooseRoute` 的 `bindable==false` 分支（`portalroute.go:71-75`），
  **需防抖**（单飞 + 最小间隔，避免 Bindable 抖动风暴）。

### 接口 C：`OnEgressInvalid` 装配移出 `if p.rd != nil`（`compose_daemon.go:53-65`）
- 有 rd（自动发现）→ 原样 `picker.Down + rd.force`；
- 无 rd（注入/钉源）→ 调 `sourceStale`（无 sink 则 no-op，Windows 行为零变化）。
- 这是打开「最强信号在注入模式丢失」的一刀。

### 接口 D（可选，v2.5）：状态文案诚实化
- `egressDetail` 在无自愈缝时装注入模式改「等待网络/重新注入源」，不写「正在重选」。
- 本轮**不做**（surgical changes 原则），除非你要求。

## 3. 行为清单（tracer bullet 垂直切片，一次一测试一实现）

> 测试通过公开接口、不 mock 内部协作者；每个 RED→GREEN 一循环。
> 位置：`internal/app/` 用 Go 测试，`mobile/` 用现有注入缝替身。

1. **T1 · 重注入改变实际绑源**（tracer bullet）
   - 测试：Compose 注入 v4=A → 重注入 v4=B → 断言 `plumbing.currentV4()` 与四客户端
     `Source()` 都变 B（通过公开快照接口读，不查私有字段）。
   - 实现：接口 A（注册 + 重绑闭包复用 `compose_portal.go:237-245`）。
2. **T2 · 注入模式下出口失效触发 SourceStale 一次**
   - 测试：注入全部死亡（`Bindable` 替身返回 false）→ 连续多拍 → 断言 SourceStale
     只触发一次（防抖）。
   - 实现：接口 C + 接口 B（含防抖）。
3. **T3 · SourceStale 后重注入能恢复绑源**
   - 测试：T2 后替身恢复 v4=B → 经重绑 → 断言快照 Source=B、DNS 缓存被清。
   - 实现：缝合 T1/T2，验证端到端。
4. **T4 · Windows 自动发现行为零回归**
   - 测试：非注入模式（有 rd）下 OnEgressInvalid 仍走原 `picker.Down + rd.force`；
     `net_mode=system/bound` 不变。
   - 实现：确认接口 C 的分支隔离（不需要新代码则跳过）。

## 4. 验收判据

- **单元测试**：`cd go-cnal && go test ./... -count=1` 全绿（当前基线全绿，不许变红）。
- **真机复测**（安卓 debug 签名版，端口 40781）：换 WiFi / 路由器重启后，无需重启守护，
  1 拍内恢复在线；观测 `cnalw.log` 的 `物理源切换:` 行 + `/state` 的 `v4_src`。
- **注意测试陷阱**：测量途中不要设 debug app（MIUI 会 force-stop）；先设端口转发
  `adb forward`，用 UI 按钮测，断档按"人为"标注不计入。
- **审查**：trouble-maker 子智能体逐行审查 diff；tester 子智能体跑全量测试。
- **发布**：本轮**不自动发布**；改动完成 + 测试全绿 + 审查通过后，由用户确认发版范围
  （tag / Release / APK 打包）。

## 5. 交付顺序（多用子智能体）

1. 用户批准本计划（接口 A/B/C 的选择）
2. TDD 实现（我主导，垂直切片 T1→T2→T3→T4）
3. 审查子智能体（trouble-maker）→ 修问题 → tester 子智能体验证
4. 真机复测（若设备在线）
5. 用户确认后才发布

## 6. 本轮明确不做（surgical changes）

- 不改 `chooseRoute` 选路语义、不动 keeper 状态机、不改 `Client.Bindable` 实现。
- 不做 ① v4 全源搬移 / ② extra v6 / ④ 配置导入 / 异校适配（下个版本）。
- 不碰 `net_mode=system/bound` 的既有行为。
