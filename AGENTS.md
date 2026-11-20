# campus-net-autologin（Python 校园网守护）

工作区级规则见 `../AGENTS.md`（测试门、git 守则、破坏性操作、凭据纪律），此处只写本项目特有的坑。

## 入口与测试

- 入口：`python app_main.py`
- 测试门：`python tests_tdd.py`（行为级 TDD，真实门户项默认 SKIP，`CNA_LIVE_TESTS=1` 开启）
- 离线冒烟：`python -I smoke_test.py`（不联网、不改状态）

## 项目特有的坑

- **探测/登录必须走物理栈**：绑定物理网卡源 IP + 校园网 DNS 解析后钉 IP，绕开 Clash/FlClash 的 TUN fake-ip。不要改回朴素 `requests`/系统 DNS——曾因 TUN 漏判一次时段切换。
- **凭据只在 keyring**：真实密码存 Windows 凭据管理器，`config.ini` 只留 `<keyring>` 占位。`config.ini`、`shot_*.png`（含内网 IP）、`keeper.log*`、`cookies.pkl`、`credentials.json` 均 gitignored，禁止提交真实凭据或内网 IP。
- **文档不得说未发布的话**：最新提交 `d73f6a0` 就是修 README 的对外 v6 假话。改 README 前核对实际发布版本，禁止宣称未复现/未发布能力。异校词表、双栈对端参数（`v6_peer`/`peer_opr`/`peer_path`）须实测再写，留空即关闭。
- **出包走 onedir**：`build_exe.py` + `.spec`（onedir 非 onefile，降杀毒误报）；`dist/`、`build/` 不入库。

## 路标

- 物理栈与双栈设计决策（E1–E16 逐条带 `keeper.py:行号`）：`docs/superpowers/specs/2026-11-06-dualstack-and-go-port-design.md`
- 有线口认证机制与双通道守护升级方案（2026-11-20 实测）：`docs/superpowers/specs/2026-11-20-wired-port-auth-and-dual-channel-guard.md`
- 异校实测报告模板：`.github/ISSUE_TEMPLATE/compat_report.md`
- 用户文档（中文权威）：`README.md`
