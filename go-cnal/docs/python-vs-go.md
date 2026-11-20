# Python 版 vs Go 版：功能、性能、资源、通用性对比

> **历史档案（2026-11-08 对比记录）**：Python 版已于 2026-11-20 退役，本页仅作
> 技术演进记录保留；文中提到的 `keeper.py`/`tests_tdd.py`/`compat_check.py`
> 等文件已从仓库删除。
>
> 2026-11-08。Python 版 `campus-net-autologin` v1.0.14（dist 打包）；
> Go 版 `go-cnal` HEAD `58aa73e`。同一台机器、同一份
> `%LOCALAPPDATA%\campus-net-autologin\config.ini`、同一账号、会话在线；
> 两个版本**分时**启动，不并行（避免互抢登录）。

## 0. 结论

- **Go 版领先**：部署形态（3 个自包含 exe / 无 Python 运行时）、物理栈钉 IP、
  双栈补绑（`/rebindv6`）、OS 级单实例锁、跨端（Android 方案）、测试分层。
- **Python 版领先**：首跑向导的交互细节（向导内「自动探测」按钮）；
  配置面、`[response]` 词表、退避可调、离线兼容自检已对齐。
- **按“通用性 / 实用性”判**：Go 版 P0 三项（`[response]` 词表、
  `reject_backoff/transient_backoff` 可配、首跑探测/配置面板）与 P1 三项
  （面板高级设置区、`cnal compat`、浏览器打开面板）已于 2026-11-08 补齐
  （§9/§10）；P2 的 `cnal status` 与迁移文档同日补齐（§11），Android
  A1–A5 测试计划就绪（真机未执行）；`login_form` 表单模板与面板分栏同日
  补齐（§12）。真实踢线时延按“不影响正常上网”要求未主动制造，改由真实
  踢线时从日志被动计算。
- **资源**：Go 托盘闲置更省 Working Set（26.3 vs 68.2 MB），但私有内存更高
  （55.5 vs 44.3 MB）；Go 打开 WebView2 面板时 7 进程 **359.7 MB WS /
  209.9 MB private**，Python Tkinter 面板是同进程（未单独测，无 WebView2
  子进程）。
- **启动到首状态日志**：Go 1.67s，Python 2.72s。

## 1. 测量口径

| 项 | 方法 |
|---|---|
| 环境 | Windows 10.0.26300 x64；同一账号；会话在线；测量期间不触发登录 |
| Python | `dist/campus-net-autologin/campus-net-autologin.exe`（PyInstaller one-folder） |
| Go | `%LOCALAPPDATA%\Programs\cnal\cnalw.exe`（HEAD 重建安装） |
| 内存/CPU | 启动预热 30s，采样两次，间隔 30s；CPU 取两次 `CPU` 秒差 / 30s（单核百分比）；进程树含子进程（Go 面板含 WebView2） |
| 启动 | 进程启动 → 日志首次出现 `状态:` 行的时间 |
| 安装体积 | 目录/zip 递归文件字节和；文件数 |
| 测试 | Python `python tests_tdd.py`；Go `go test -count=1 ./...` |

## 2. 资源与性能（实测）

| 指标 | Python v1.0.14 | Go HEAD | 说明 |
|---|---:|---:|---|
| 安装体积 | **53.9 MB / 1015 文件** | **zip 18.16 MB；装后 3 exe 33.2 MB** | Go 自包含；Python one-folder |
| 主程序 | 6.55 MB exe + `_internal` | cnalw 12.6 MB（cnal 9.3 / cnald 11.3） | Go 单文件无运行时 |
| 托盘闲置 Working Set | 68.2 MB | **26.3 MB** | Go 更省 WS |
| 托盘闲置 Private | 44.3 MB | 55.5 MB | Go 私有内存略高 |
| Handles | 345 | 479 | Go 多 |
| Threads | 9–12 | 24–27 | Go runtime 线程多 |
| 闲置 CPU（单核） | 0.10% | 0.27% | 3s tick，两者都低 |
| 面板打开 Working Set | 未单独测（Tkinter 同进程） | **359.7 MB（7 进程）** | WebView2 子进程是主因 |
| 面板打开 Private | 未单独测 | **209.9 MB** | 同上 |
| 面板打开 Handles/Threads | — | 3774 / 225 | 同上 |
| 启动到首状态 | 2.72 s | **1.67 s** | 同一日志口径 |
| 测试 | 154 PASS / 0 FAIL / 1 SKIP | 9 包全绿 / 306 个 Test 函数 | Python 真实门户项 SKIP；Go 为 2026-11-10 计数（含会话探针节流修复新增的测试） |

**读法**：Go 的托盘常驻更轻（WS 26 vs 68 MB），但它的面板走 WebView2，
打开时比 Python 的 Tkinter 重一个量级。面板是按需打开的；如果用户长时间
开着面板，Go 的实际占用会高于 Python。

## 3. 功能对比

| 能力 | Python | Go | 判定 |
|---|---|---|---|
| 登录/守护/风暴三闸 | ✅ | ✅ | 对齐 |
| 物理栈绑源 + 门户钉真实 IP | ✅ | ✅ | 对齐（Go `portalTarget` fail-closed） |
| 双栈登录 `ipv4or6` | ✅ 配置项 | ✅ | 对齐 |
| v6 四态观察 | ✅ | ✅ | 对齐 |
| **v6 主动补绑** | ❌ 只观察 | ✅ `/rebindv6` + 去抖 | **Go 领先** |
| flux 查流量 | ✅ | ✅ 面板显示真实字段 | 对齐（Go 面板刚修好键名） |
| 日志可视化 | ✅ Tkinter | ✅ Web 面板 `/logs` | 对齐 |
| 账号历史/热切换 | ✅ | ✅ | 对齐 |
| **免认证时段编辑** | ❌ 只能改 ini | ✅ 结构化 time 编辑器 + 双闸 + 热生效 | **Go 领先** |
| **面板配置面** | 14 项网络/高级参数 + 账号/密码/服务器 | 6 项常用 + 15 项高级折叠区 | 对齐（P1 已补） |
| **首跑向导/门户探测** | ✅ 首次运行引导 + 自动检测门户 | ✅ `cnal detect` + 缺配置/凭据开首跑面板 | 对齐（P0 已补） |
| **异校响应词表** | ✅ `[response]` 5 组词表 | ✅ `[response]` 4 组词表（hijack 仍走锚点配置） | 对齐（P0 已补） |
| **异校登录字段名** | ❌ `FORM_BASE` 固定 `opr/userName/pwd/rememberPwd` | ✅ `login_form` 模板（`{user}`/`{pass}` 占位） | **Go 领先**（2026-11-08） |
| **退避参数可调** | ✅ `reject_backoff`/`transient_backoff`/`portal_down_wait` | ✅ 全部可配（含瞬态连击升级） | 对齐（P0 已补） |
| **兼容性自检** | ✅ `compat_check.py --detect` + 响应判定 | ✅ `cnal compat`（离线判定 + 协议指纹） | 对齐（P1 已补） |
| 托盘/开机自启 | ✅ | ✅ | 对齐 |
| 单实例 | ✅ 锁文件 | ✅ LockFileEx OS 字节锁 | Go 更稳（进程死自动释放） |
| CLI | `compat_check.py` / 守护 | `cnal status` / `rebind` / `detect` / `compat` | Go 诊断面更全（P2 补 status） |
| Clash API 恢复 | ✅ 可选（默认关） | ❌ 明确不移植 | 按规格：不算缺口 |
| Android | ❌ Windows only | ✅ 方案 + 纯 Go 内核可复用 | **Go 领先** |
| 安装 | Python 3.10+ / pip / PyInstaller | 3 exe，无运行时 | **Go 领先** |
| 文档/踩坑沉淀 | 20KB README + 规格 + 154 断言 | README/AGENTS/DESIGN + 306 Test + 调研 | 各有侧重 |

**通用性口径（2026-11-09 校正）**：两版都只支持 **ac_portal 族**。上表
「异校可配」项（端点/词表/`login_form`/探测锚点）由表驱动测试覆盖，
但**只在参考校真机验证过**——异校实际可用性属推断，未经事实检验；
Srun/eportal/Dr.COM 仅能指纹识别，不支持登录。

## 4. 通用性：配置面差距（核心）

Python `DEFAULTS` 27 个键；Go `daemonConfig` 覆盖大部分端点/探测/双栈键，
但以下**异校会踩**的键 Go 没有或没上面板：

| 键 | Python | Go 配置 | Go 面板 |
|---|---|---|---|
| `response.success_marks / negative_marks / transient_marks / freeze_marks` | ✅ `[response]` | ✅ `[response]` | ✅ 高级区 |
| `login_form`（字段名/操作名/静态参数模板） | ❌ 固定 | ✅ ini | ✅ 高级区 |
| `reject_backoff` / `transient_backoff` | ✅ 面板可改 | ✅ ini | ✅ 高级区 |
| `campus_dns` | ✅ 面板 | ✅ | ✅ 高级区 |
| `interval` / `probe_timeout` / `login_timeout` | ✅ 面板 | ✅ | ✅ 高级区 |
| `probe_ips` | ✅ 面板 | ✅ | ✅ 高级区 |
| `login_page` / `user_agent` / `ssl_verify` | ✅ 面板 | ✅ | ✅ 高级区 |
| `portal_down_wait` | ✅ 面板 | ✅ | ✅ 高级区 |
| `free_windows` | ✅ ini（❌ 面板） | ✅ | ✅ 结构化编辑器 |
| `bind_ip` / `auto_reconnect` | ✅ 面板 | ✅ | ✅ |
| `clash_api_url/secret` | ✅ 面板 | ❌ 明确不移植 | ❌ |
| `dual_stack/v6_peer/peer_opr/peer_path` | ✅ ini | ✅ | ❌（仅 ini） |

结论：P1 之后 Go 的**可发现/可编辑**面板面已与 Python 基本对齐（15 项高级
折叠区）；仅 `clash_api_*`（明确不移植）与双栈四键（仅 ini）不在面板。
异校用户不编辑 ini 也能完成主要调参，通用性缺口关闭。

## 5. 实用性：新机、日常、资源

1. **新机首跑**：Go 已补 `cnal detect`（302 Location / 200 劫持页正文 +
   `ac_portal` 指纹复核）与首跑配置面板；缺配置/凭据时托盘不再报错退出，
   而是探测门户、打开面板收集账号密码。异校仍建议核对端点与 `[response]`。
2. **日常调参**：Go 面板 6 项常用 + 15 项高级折叠区（含 `[response]` 词表），
   与 Python 的 14 项 + 高级窗基本对齐；双栈词表仍需改 ini。
3. **资源**：Go 托盘更轻；面板重（WebView2 359.7MB）。已补 `panel_mode=browser`
   与托盘「在浏览器打开面板」一次性入口，WebView2 可按需/关闭。
4. **可靠性**：两版都有风暴闸/退避/免会话检测；Go 多了 OS 单实例锁与 v6
   补绑；Python 的测试语料更大（154 断言）。
5. **升级/迁移**：Go 复用 Python 的 `config.ini` 与 keyring，迁移成本低；
   首跑不再依赖 Python（`cnal detect --write` / 首跑配置面板）。切换与回滚
   步骤见 `docs/migration-python-to-go.md`。

## 6. 建议（按优先级）

### P0（通用性，异校会直接踩）——2026-11-08 已全部落地，见 §9

1. ✅ **Go 支持 `[response]` 词表**（success/negative/transient/freeze），
   与 Python `RESPONSE_DEFAULTS` 同语义；表驱动测试覆盖 JSON/伪 JSON/
   `ret_code`/纯文本（含长响应护栏）。
2. ✅ **Go 支持 `reject_backoff`/`transient_backoff`**（写进 `daemonConfig`，
   传入 Daemon；保留 30/600 默认，范围按 Python `_num` 钳制；瞬态连续 5 次
   升级长退避；冻结闸下限取 `reject_backoff`）。
3. ✅ **Go 首跑路径**：`cnal detect`（含 `--detect` 别名、`--write`）+
   首跑配置面板；缺配置/凭据不再报错退出，探测只做无凭据 GET 并指纹复核。

### P1（实用性）——2026-11-08 已全部落地，见 §10

4. ✅ **Go 面板补全 ini 已有字段**（可折叠“高级设置”）：campus_dns、interval、
   probe_timeout、login_timeout、probe_ips、login_page、user_agent、
   ssl_verify、portal_down_wait、reject_backoff、transient_backoff 与
   `[response]` 四组词表；写入按节分派（词表进 `[response]` 段）。
5. ✅ **Go `cnal compat`**：离线响应判定 + 协议指纹，等价 `compat_check.py`；
   与守护共用 `JudgeLoginResponse`/`ResponseMarks` 与同一份 ini。
6. ✅ **面板内存**：`panel_mode=browser` + 托盘「在浏览器打开面板」入口；
   WebView2 可按需/关闭。

### P2——2026-11-08 部分落地，见 §11

7. ✅ `cnal status` / `--status`（只读本机回路，不触网、不触发登录）；
   ✅ Python→Go 迁移文档（配置/凭据/回滚）。
8. ⏸ 真实踢线恢复时延对比：**未做**——主动踢线会中断用户正常上网，
   本轮按用户要求不制造踢线；改为真实踢线时从 `cnalw.log` 的
   `离线 → 在线` 时间戳被动计算。
   ⏸ Android A1–A5：测试计划与记录模板已写（`docs/android-a1-a5-testplan.md`），
   真机未执行（本机无 Android 真机/校园 Wi-Fi 环境）。

## 7. 未测 / 边界

- 未模拟“门户踢线”测实时恢复时延；两版 interval/probe/login 超时默认相同
  （3s/3s/6s），架构上同级，但未做同场景秒表对比。2026-11-08 按“不影响
  正常上网”要求未主动踢线，被动测量口径：`cnalw.log` 里同一会话
  `offline → online` 两条状态行的时间差。
- 未测登录请求时延（网络后端主导，差异应在毫秒级）。
- Python 面板打开时的内存未单独测（Tkinter 同进程，无 WebView2 子进程）。
- Mimosa 全量审计未完成（python_ast/library_source 缺失）；本对比不构成
  安全结论。
- Python 154 PASS / 1 SKIP 中的 SKIP 是真实门户项；Go 的真实门户验收由
  `release_verify.py` 覆盖（2026-11-09 实跑 31/31；脚本此后新增断言，
  发布前需重跑并更新实跑数）。

## 8. 证据（可复跑）

```text
# Python 测试
python tests_tdd.py
→ 结果: PASS=154 FAIL=0 SKIP=1（真实门户项未启用）

# Go 测试
go vet ./...
go test -count=1 ./...        # 9 包全绿

# 资源（临时脚本，未入库）
python-idle : WS 68.2MB  Private 44.3MB  Handles 345  Threads 12  CPU 0.10%
go-idle     : WS 26.3MB  Private 55.5MB  Handles 479  Threads 24  CPU 0.27%
go-panel    : WS 359.7MB Private 209.9MB Handles 3774 Threads 225 CPU 0.93% (7 进程)

# 启动到首个“状态:”日志
python-start: 2.72s
go-start    : 1.67s

# 安装体积
python dist : 53.9MB / 1015 files
go zip      : 18.16MB；安装后 3 exe 33.2MB
```

## 9. P0 落地（2026-11-08）

| P0 | 实现 | 验证 |
|---|---|---|
| `[response]` 词表 | `keeper.ResponseMarks` + `JudgeLoginResponse`（JSON/伪 JSON/`ret_code`/纯文本，长响应护栏）；app/CLI 都解析 `[response]`（含 Python 的 `hijack_marks`）；守护与 CLI 共用判定入口 | `TestJudgeLoginResponseDefaults`、`TestDefaultMarksMatchPython`、`TestConfigResponseMarksAndBackoff` |
| 退避可配 | `daemonConfig.RejectBackoff/TransientBackoff` → `Daemon.SetBackoff`；瞬态连续 5 次升级长退避；冻结闸下限取 `reject_backoff` | `TestBackoffConfigurableHardVsTransient`、`TestTransientStreakEscalatesToRejectBackoff`、`TestFreezeGateUsesConfiguredRejectBackoff` |
| 首跑路径 | `cnal detect`（302 Location / 200 正文绝对 URL + `ac_portal` 指纹复核，全程无凭据 GET）；`app.NewSetup` 首跑面板；缺配置/凭据 → `SetupError` 进面板而非报错退出 | `TestParsePortalLocation`、`TestPortalBaseFromBody`、`TestWriteStarterConfig`、`TestComposeMissingConfigIsSetupError` |

调研依据见 `docs/campus-portal-survey.md`：协议族割裂（ac_portal/eportal/Srun
等不可互适配）、`result` 0/1 语义随校反转（不做全局判据）、夜间措施公开事实
是断网而非免认证。

## 10. P1 落地（2026-11-08 同日）

| P1 | 实现 | 验证 |
|---|---|---|
| 面板高级设置区 | `internal/app/advanced.go`：`advancedConfigView` 暴露 ini 已有字段；`validateAdvancedConfig` 数值/布尔/白名单校验；`writeAdvancedConfig` 按节写入（词表进 `[response]`，数值进 `[keeper]`）；面板用原生 `<details>` 折叠，沿用 DESIGN.md 既有组件 | `TestAdvancedConfigView`、`TestValidateAdvancedConfigRejectsBadNumbers`、`TestWriteAdvancedConfigUpdatesExistingResponseSection`、`TestSaveConfigValuesRejectsInvalidBeforeWrite` |
| `cnal compat` | `keeper.CompatCheck`/`ProtocolFingerprint`（离线、零凭据）；CLI 支持位置样本、`--file`、`--detect`、`--config`，且 flag 可放在样本之后 | `TestCompatCheck*`、`TestProtocolFingerprint*`、`TestRunCompatCmdAcceptsFlagAfterSample` |
| 浏览器打开面板 | `panel_mode=native/browser`（缺省 native，非法值回落）；托盘新增「在浏览器打开面板」；`--open-panel` 也按模式分派；native 失败仍回落浏览器 | `TestPanelModeFromIniAndEnv`、`TestOpenPanelByModeBrowser`、`TestOpenPanelByModeNativeFallsBackOnError` |

真浏览器验收：`release_verify.py` 31/31（新增分栏/懒加载/安全头/登录表单
回填八项），截图 `docs/screenshots/release-advanced.png`。

**P2 状态（2026-11-08）**：`cnal status` 与迁移文档已落地；Android A1–A5
测试计划就绪、真机未执行；真实踢线时延按“不影响正常上网”要求未主动测。
详见 §11。

## 11. P2 落地（2026-11-08 同日）

| P2 | 实现 | 验证 |
|---|---|---|
| `cnal status` / `--status` | 守护启动写 `%LOCALAPPDATA%\campus-net-autologin\api.json`（127.0.0.1 端口 + 随机 token，不含凭据，退出删除）；CLI 只读 `GET /state`，缺文件/坏 JSON/非 200 一律报「未运行/未响应」并退 7，不猜端点、不触发登录 | `TestRuntimeFileRoundTrip`、`TestReadRuntimeFileRejectsIncomplete`、`TestFetchStatusSendsBearerAndDecodes`、`TestStatusReportEndToEnd`、`TestDispatchStatusAliases` |
| 迁移文档 | `docs/migration-python-to-go.md`：共用资源、切换/回滚步骤、配置差异（`ssl_verify` 默认更严、clash 不移植、明文密码不读）、排障、禁止双守护 | 命令按本机路径核对；不涉及真实凭据 |
| Android A1–A5 | `docs/android-a1-a5-testplan.md`：A1–A5 步骤、通过线、记录模板、判定降级矩阵；明确「未执行」及原因（无真机/校园 Wi-Fi/工具链） | 计划与规格 §11 对齐；真机数据待补 |
| 真实踢线时延 | **未做**：主动踢线会中断正常上网；被动口径写入 §7（`cnalw.log` 的 `offline → online` 时间差） | — |

`cnal status` 的只读性由代码路径保证：只读端点文件 + `GET /state`，无
`/login` 调用、无门户请求；`--status` 别名与 `status` 同一实现。

## 12. 通用性补强（2026-11-08 同日）

| 项 | 实现 | 验证 |
|---|---|---|
| ac_portal 表单模板化 | `protocol.Form` / `ParseForm`：`login_form = opr=login&user_account={user}&user_password={pass}`；默认模板与 Python `FORM_BASE` 逐字同形；守护自动登录、v6 补绑、CLI rebind 三路共用 | `TestParseFormRejectsInvalid`、`TestDefaultFormMatchesPython`、`TestLoginActionUsesConfiguredForm`、`TestDoRebindUsesConfiguredForm` |
| 面板状态/设置分栏 | 文字 tab + 2px 下划线；设置页首次打开才拉 `/config` 与 `/accounts`；状态页 5s 轮询在隐藏/切页时暂停 | `TestPanelPageHasTabs`、`TestPanelSettingsLazyLoaded`、release_verify 31/31 |
| 面板安全头/触控 | `/panel` 加 CSP（default-src none + inline/self）、`no-store`、`nosniff`；`pointer: coarse` 可点面 44px | `TestPanelSendsSecurityHeaders`、`TestPanelCSSHasTouchTargets` |

边界不变：这仍是 **ac_portal 族内** 的通用性补强；Srun/eportal/DrCOM 等
协议族仍需适配层，不在本轮范围。
