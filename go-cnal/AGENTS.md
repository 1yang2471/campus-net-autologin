# go-cnal（Go 重写校园网登录引擎）

Python 守护（`../campus-net-autologin`）的 Go 重写：ac_portal 族协议、物理栈探测、
双栈登录、守护状态机。**Windows 侧已换役服役**（cnalw 托盘 + 自启）。

完整实测故事已迁至 `docs/AGENTS-坑位实测故事.md`（逐字保留）；本表只留「规则 → 由什么拦住」。

## 工作方式

- 测试：`go test ./...`（9 包）；`go vet ./...`；改语义必须红→绿→变异检查。
- **改动收尾自动更新安装版**：`scripts\update.ps1`（构建+安装+哈希验收；运行中的
  托盘不打断，新 exe 下次启动生效，`-Restart` 才强制重启）。`.git/hooks/post-commit`
  已接同一脚本（源文件 `scripts/hooks/post-commit`，新克隆按 README 装上）：
  `cmd/`/`internal/`/`mobile/`/构建脚本的提交会自动跑一次。
- **变异检查还原只用当场指纹/备份，禁用 `git checkout`**（会吞掉未提交的改动）。
- 提交被 `scripts/check-file-size.sh`（钩子已装在 `.git/hooks/pre-commit`）拒了：
  照报错把文件按功能拆小再提交，别调大上限。
- 写新函数/新规则前先搜项目里有没有现成的（例：握手构造收敛为 app 的
  `handshakeOnce`；v6 文案唯一出处是 `keeper.V6Label`）。
- **判据先量再写**：截图里看到的"残片/异常"先回放真实数据确认是数据 bug 还是
  渲染裁切，再改代码；没被红灯证明的改动与"构造没复现却通过"的测试都撤回
  （2026-11-08 日志卡 TISW 残影误判，已撤回投机改动与假测试）。

完整实测故事（坑 12–21）已迁至 `docs/AGENTS-坑位实测故事.md`，此处不重复。

## 规则表

| 规则 | 由什么拦住 |
|---|---|
| 交差前跑通测试，附截图 | `scripts/release_verify.py`（真 Chrome 面板全流程断言 + 四张截图） |
| 异校 UA/Referer 单一出处 | `TestLoadConfigUserAgentAndLoginPage`（CLI 吃 ini；缺省取 `protocol.DefaultUserAgent`） |
| cnalw 必须 GUI 子系统 | `scripts/install.ps1` 复制前验 PE 子系统字节 |
| 门户必须钉真实 IP | `app.portalTarget` + `internal/app/portaltarget_test.go` |
| 空闲态图标 16px 可见 | `TestIdleIconVisibleAtTraySize`（覆盖率 ≥25%） |
| 展示字段无数据竞争 | `Daemon.View()`/`StateForUI()` 加锁读，Tick 写走 setState |
| LF ini 不被压扁 | `TestUpdateIniFieldsLFPreservesLines` |
| 空密码不落凭据 | `TestPostConfigEmptyPasswordRejected` + `TestSetPasswordRejectsEmpty`（api/store 双闸） |
| query token 只放行 /panel | `TestQueryTokenOnlyForPanel` |
| 词表默认值不漂移 | `TestDefaultMarksMatchPython` + `TestConfigResponseAndBackoffDefaults` |
| 退避可配与瞬态升级 | `TestBackoffConfigurableHardVsTransient` + `TestTransientStreakEscalatesToRejectBackoff` |
| 首跑探测不猜地址/不发凭据 | `TestParsePortalLocation` + `TestPortalBaseFromBody` + `TestWriteStarterConfig` |
| 高级配置分节写入/不半写 | `TestWriteAdvancedConfigUpdatesExistingResponseSection` + `TestSaveConfigValuesRejectsInvalidBeforeWrite` |
| 兼容自检与守护同词表 | `TestCompatMarksFromIni` + `TestCompatCheckTransientAndCustomMarks` |
| CLI flag 可在样本后 | `TestRunCompatCmdAcceptsFlagAfterSample` |
| 面板模式缺省 native | `TestOpenPanelByModeBrowser` + `TestPanelModeFromIniAndEnv` |
| status 只读且不猜端点 | `TestStatusReportNoDaemon` + `TestReadRuntimeFileRejectsIncomplete` + `TestDispatchStatusAliases` |
| 异校登录表单三路同源 | `TestParseFormRejectsInvalid` + `TestLoginActionUsesConfiguredForm` + `TestDoRebindUsesConfiguredForm` |
| 切账号必须认证一次且降级不静默 | `TestSwitchAccountAuthenticatesWithNewAccount` + `TestDoReportsV6DegradedOnHandshakeFail` + `TestSwitchAccountReportsDegradedV6` |
| 异校判定层覆盖度不静默回退 | `TestCorpusReplayDefaultMarksSufficient` + `TestCorpusReplayMarksOverrideNecessaryAndSufficient` + `TestCorpusReplayUnfixableGapsAreReal`（覆盖度漂移即红；语料与来源见 `docs/corpus-异校响应语料.md`） |

## 路标

- 设计规格（双栈语义、E4/E7/E19、§5.1 四态、§11 安卓前提 A1-A5）：
  `../campus-net-autologin/docs/superpowers/specs/2026-11-06-dualstack-and-go-port-design.md`
- `docs/screenshots/`：界面验收证据（托盘四态图标等）。
- 回路契约：`internal/api`（127.0.0.1 + 随机 token，面板/托盘同一来源）。

<!-- claimrail:begin -->
## claimrail

动界面、文档、接口之前，先跑 `claimrail skill` 读协议（它从安装包自身打印，不依赖本仓库的 node_modules），
写 `.claimrail/decisions.json` 申报分叉与证据；
交付前跑 `claimrail check`——结构不合规先修申报，红的不许交。
命令不存在时：到 claimrail 源码目录 npm link，或用 node 直接跑它的 src/cli.mjs。
<!-- claimrail:end -->
