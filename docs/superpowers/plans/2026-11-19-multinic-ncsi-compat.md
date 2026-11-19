# 多网卡观察 + NCSI 说明 + 兼容模板 实施计划（2026-11-19）

> 目标：把 2026-11-19 复盘（双网卡未认证 → Windows「无 Internet」误报）落成产品能力。
> 测试门：`go test ./...`、`python tests_tdd.py`、`python -I smoke_test.py`。

## 背景（实测）

- keeper 只维护**一个** `bind_ip`；网线插在未认证 VLAN 时永远不被触碰，
  面板也看不出来（2026-11-19 10:14-11:14 实测：WiFi 已认证、以太网 443 全拒、
  NCSI 探测被 302 劫持）。
- Windows 双网卡并存时联网指示取最差结果 → 黄叹号 ≠ 断网。
- NCSI `EnableActiveProbing` 需管理员写 HKLM；自动开关会弹 UAC、跨睡眠不可控，
  **不产品化改注册表**，改为在面板把「另一网卡未认证」这个事实说清楚。

## 任务

### Task 1: 内核观察缝（go-cnal/internal/keeper）
- 新增缝 `ProbeSiblingAuth func() []string`：返回「已连接但未认证」的非绑定源。
- Tick 顶部与 v6/L2 同层节流（60s）调用；结果 `UnauthSiblings()` 加锁读。
- 只观察，**绝不**在这些卡上登录（登录出口仍只有当前绑定源）。

### Task 2: 组合根探测（go-cnal/internal/app）
- 纯函数 `siblingVerdict(status, location, body, portalHost, hijackMarks)`：
  3xx→门户 / 200+劫持指纹 = unauth；2xx/其它 3xx = authed；错误 = unknown。
- `plumbing.siblingUnauth`：`CandidateSources` 去掉当前绑定源，最多探 2 个；
  用绑源 DNS 解析 `www.baidu.com`（失败回退系统解析，观测无凭据），
  HTTP 不跟随跳转取状态码与 Location/body。
- compose_daemon 接线：安卓注入源模式跳过（Kotlin 管网络）。

### Task 3: 面板与 API
- `api.Snapshot` 新增 `unauth_sources`（omitempty）；compose_api 填 `d.UnauthSiblings()`。
- 面板诊断卡新增一行「其他网卡」：`10.141.53.34（已连接未认证；Windows 可能误报无 Internet）`。
- 面板测试守 `id="nics"` / `unauth_sources` 字样。

### Task 4: 兼容模板
- `.github/ISSUE_TEMPLATE/compat_report.md` 环境段新增：DHCP 租约时长、
  双网卡并存时 Windows 联网指示是否正常。

### Task 5: 验收与发布
- `go test ./...`、`python tests_tdd.py`、`python -I smoke_test.py`。
- `scripts/build.ps1 -Version 2.2.0` → `dist/go-cnal-2.2.0-win64.zip`。
- `scripts/release_verify.py`（真浏览器 31 项 + 截图）。
- `scripts/install.ps1` 覆盖安装并重启 cnalw。
- Android：重建 AAR + `scripts/build-release.ps1`，`adb install -r` 到已连设备。
- 发布：README 版本表更新 → tag `v2.2.0-go` → `gh release create`（zip + apk）。
