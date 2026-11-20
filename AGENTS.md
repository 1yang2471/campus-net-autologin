# campus-net-autologin（校园网守护，Go 实现）

工作区级规则见 `../AGENTS.md`（测试门、git 守则、破坏性操作、凭据纪律），此处只写本项目特有的坑。

## 入口与测试

- 主实现目录：`go-cnal/`（Windows 托盘 + 无头 + CLI 三 exe）
- 测试门：`cd go-cnal && go test ./...`
- 入口（源码跑）：`go run ./cmd/cnalw`

## 项目特有的坑

- **探测/登录必须走物理栈**：绑定物理网卡源 IP + 校园网 DNS 解析后钉 IP，绕开 Clash/FlClash 的 TUN fake-ip。不要改回朴素 `requests`/系统 DNS——曾因 TUN 漏判一次时段切换。
- **凭据只在 keyring**：真实密码存 Windows 凭据管理器，`config.ini` 只留 `<keyring>` 占位。`config.ini`、`keeper.log*`、`cookies.pkl`、`credentials.json` 均 gitignored，禁止提交真实凭据或内网 IP。
- **文档不得说未发布的话**：改 README 前核对实际发布版本，禁止宣称未复现/未发布能力。异校词表、双栈对端参数（`v6_peer`/`peer_opr`/`peer_path`）须实测再写，留空即关闭。
- **出包走 onedir**：`go-cnal/scripts/` 打包（onedir 非 onefile，降杀毒误报）；`dist/`、`build/` 不入库。

## 路标

- 事故复盘与排查记录（全部保留，按日期）：`go-cnal/docs/2026-11-*.md`
- 有线口认证机制与双通道守护升级方案（2026-11-20 实测）：`docs/superpowers/specs/2026-11-20-wired-port-auth-and-dual-channel-guard.md`
- 异校实测报告模板：`.github/ISSUE_TEMPLATE/compat_report.md`
- 用户文档（中文权威）：`README.md`
