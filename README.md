# go-cnal · 校园网守护（Go 版）

Python 守护的 Go 重写：物理栈绑源探测、ac_portal 族双栈登录、托盘 + 面板。
Windows 用户级安装，不需要管理员权限。

## 适用范围（先看这个）

- **支持**：ac_portal 族（华为 AC 系 Portal 部署）+ 账号密码登录。
- **不支持**：深澜 Srun、Dr.COM / 锐捷 eportal 等协议族。`cnal compat --detect`
  只能告诉你「这是别的协议族」，不能代替它们登录。
- **平台**：Windows。Android 版已起 M0 测量壳（独立仓库 `../go-cnal-android`，
  纯 Kotlin 不接内核），**A1–A5 真机门槛未过，产品形态未定**。
- **验证程度**：**已验证 1 所**（参考校真机）；**异校实测 0 所**。异校可配项（端点 /
  `[response]` 词表 / `login_form` 登录字段名 / 探测锚点）有表驱动测试覆盖，但
  **尚未在任何异校真实验证**——「异校可配」是设计目标，不是已验证事实。
  2026-11-12 复盘拍板：**不立项异校通用**，本工具边界按「仅参考校可用」理解，
  异校用户需自行完成字段核对并自担风险。
  **2026-11-09 补了离线覆盖度实测**：公开资料里 16 条真实应答回放进判定器，
  13 条默认词表就够、2 条写一段 `[response]` 可救、1 条词表表达不了；另有
  GET 型端点 / 密码加密 / 非 ac_portal 指纹三类本层表达不了。逐条来源见
  [`docs/corpus-异校响应语料.md`](docs/corpus-异校响应语料.md)。**这仍不是异校
  实测**：它证明"对方的应答我们读不读得懂"，不证明"在对方校园网里能连通"。

## 安装

1. 解压 zip 到任意目录。
2. `powershell -ExecutionPolicy Bypass -File scripts\install.ps1`

脚本会：把三个 exe 复制到 `%LOCALAPPDATA%\Programs\cnal\`、创建开始菜单
「校园网守护」快捷方式、登记 HKCU 开机自启。重复运行即原地更新。

配置文件沿用 Python 守护同款：`%LOCALAPPDATA%\campus-net-autologin\config.ini`
（**新装无需预先准备**：首次运行 cnalw 会自动探测门户、生成最小配置并打开
设置面板收集账号密码；也可先跑 `cnal detect --write` 看探测结果）。
密码只存 Windows 凭据管理器，ini 里 `password = <keyring>` 是占位符。

`campus_dns` 强烈建议填真值（门户钉 IP 与物理源选路都靠它；留空回退系统
DNS，TUN 环境下可能拿到 fake-ip）。**面板常用区管 6 项，高级折叠区管网络
参数与 `[response]` 词表**；双栈词表（`dual_stack`/`v6_peer`/`peer_opr`/
`peer_path`）仍需直接编辑 ini（键清单见下）。

## 三个 exe

| 文件 | 用途 |
|---|---|
| `cnalw.exe` | 托盘 + 面板（日常用这个，开始菜单指向它） |
| `cnald.exe` | 无头守护（调试/服务器场景；与托盘共用单实例锁，不能同时开） |
| `cnal.exe` | CLI 诊断工具（`cnal status` / `cnal rebind` / `cnal detect` / `cnal compat`） |

## 面板功能

- 状态/设置分栏：状态页默认可见（5s 轮询，隐藏或切到设置页即暂停）；
  设置页首次打开才读配置/账号
- 状态卡：在线/离线/门户不可达/被拒/免认证时段/自动重连已关闭
- 立即检测重连（手动意图，越过自动闸与速率熔断）
- IPv4/IPv6 源、会话探针、免会话检测、查流量、补绑 IPv6
- 历史账号切换（密码从凭据管理器按用户名还原）
- 设置：账号/密码/门户/免认证时段/绑定网卡/掉线自动重连开关
- 高级设置（可折叠）：校园 DNS、探测/登录超时、探测锚点、登录页、UA、
  证书校验、门户不可达等待、硬拒绝/瞬态退避、`[response]` 四组词表、
  登录表单模板（异校字段名变体）
- 状态日志（尾部 200 行，可视化）

托盘右键：打开面板、查流量、查看日志、打开认证页、掉线自动重连开关、
开机自启、退出。

## 异校移植（仅 ac_portal 族；未经异校实证）

`config.ini [keeper]` 里全部可配，无需改代码：

- `server` / `login_path` / `login_page` / `flux_path`：门户端点
- `campus_dns`：校园 DNS（物理源选路与门户钉 IP 解析，必填真值）
- `probe_ips` / `probe_path` / `probe_host` / `hijack_marks`：公网锚点与劫持页指纹
- `user_agent` / `ssl_verify` / `probe_timeout` / `login_timeout` / `interval`
- `free_windows`：免认证时段（夜间不自动登录，防撞门户反暴力冻结）
- `dual_stack` / `v6_peer` / `peer_opr` / `peer_path`：双栈补绑（异校可全关）
- `bind_ip`：物理源逃生口（空 = 自动发现）
- `auto_reconnect`：掉线自动重连开关（面板/托盘可热改）
- `panel_mode`：`native`（缺省，WebView2 原生窗口）/ `browser`（系统浏览器，
  省 WebView2 常驻内存）；托盘另有「在浏览器打开面板」一次性入口
- `reject_backoff` / `transient_backoff`：硬拒绝/瞬态错误的退避秒数
  （默认 600/30；Python `_num` 同款范围钳制）
- `[response]` 词表：`success_marks` / `negative_marks` / `transient_marks` /
  `freeze_marks`（逗号分隔；默认值与 Python `RESPONSE_DEFAULTS` 逐字同源）。
  异校门户响应判错时先抓一次登录响应，按词表覆盖后重启。
- `login_form`：ac_portal 登录表单模板。默认
  `opr=pwdLogin&userName={user}&pwd={pass}&rememberPwd=1`（与 Python 同形）；
  异校变体改字段名/操作名/静态参数，用 `{user}`/`{pass}` 占位，例如
  `opr=login&user_account={user}&user_password={pass}&lang=zh`。
  `ipv4or6` 是双栈保留键，不得写进模板。

### 首跑 / 换学校

```text
cnal detect                      # 免凭据探测：未认证时 302 Location 自报门户
cnal detect --write              # 探测成功且配置不存在时写最小 [keeper] 配置
cnal detect --config <ini>       # 复用已有配置的 campus_dns/probe_* 再探测
cnalw --config <ini>             # 缺配置/凭据时自动进入首个配置面板

cnal compat "<登录响应>"          # 离线判定：成功/拒绝/瞬态 + 命中规则
cnal compat --detect --file page.html   # 离线协议指纹：ac_portal 族/变体/异协议

cnal status                      # 只读查看运行中守护的状态（走本机回路，不触网、不触发登录）
```

探测只做无凭据 GET，并用 `ac_portal`/`pwdLogin` 指纹复核；已登录/不在校园网
时探测会失败（没有劫持页可看），此时在面板或 ini 手工填 `server` 即可。

状态词表在 `internal/api`、形状在 `DESIGN.md`——单一出处，别在装配层自抄。

## 构建（开发）

`powershell -File scripts\build.ps1`：vet + test + 三 exe + 发布 zip。
`cnalw.exe` 必须带 `-ldflags "-H windowsgui"`（GUI 子系统），否则点开始菜单
会弹黑终端；`scripts\install.ps1` 会验 PE 子系统字节，console exe 拒装。

日常更新用 `powershell -File scripts\update.ps1`：构建 + 安装一条龙。正在运行的
托盘不打断，新 exe 下次启动生效（加 `-Restart` 才立即重启，会短暂断网）。
仓库的 post-commit 钩子在 `cmd/`、`internal/`、`mobile/` 或构建脚本改动后会跑
同一脚本，安装版不会落后于源码。新克隆启用它：

```powershell
Copy-Item scripts\hooks\post-commit .git\hooks\post-commit
```

发布前验收（真 Chrome 走一遍面板全流程断言 + 四张截图）：
`python -I scripts\release_verify.py`（需 `pip install playwright` 与本机 Chrome）。

设计规格、踩坑清单、回路契约见 `DESIGN.md` / `AGENTS.md` / `internal/api`；
安卓版方案见 `docs/android-plan.md`（A1–A5 真机测试计划
`docs/android-a1-a5-testplan.md`）；Python/Go 两版对比见
`docs/python-vs-go.md`，从 Python 版迁移见 `docs/migration-python-to-go.md`。
