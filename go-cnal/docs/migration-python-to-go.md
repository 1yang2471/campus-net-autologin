# Python 版 → Go 版迁移指南

> 2026-11-08。适用：已装 Python 版 `campus-net-autologin`（含 PyInstaller dist），
> 要换成 `go-cnal`。原则：**共用同一份配置与凭据，不迁移数据；切换可逆**。
> 两版**绝不可同时运行**（会互抢登录撞门户反暴力冻结）。

## 0. 一句话结论

停 Python → 装 Go → 验一次 → 换自启。配置键、凭据服务名、日志目录都沿用，
回滚只是反向操作；唯一需要人工确认的是自启登记项（两版值名不同，不能同时开）。

## 1. 共用什么（不复制、不转换）

| 资源 | 位置/标识 | 说明 |
|---|---|---|
| 配置 | `%LOCALAPPDATA%\campus-net-autologin\config.ini` | 同一份 `[keeper]` / `[portal]` / `[response]`；Go 新增键向后兼容 |
| 凭据 | Windows 凭据管理器，服务名 `campus-net-autologin` | 两版按用户名分条读写，同一账号无需重录 |
| 数据目录 | `%LOCALAPPDATA%\campus-net-autologin\` | 锁文件、日志、端点文件同目录 |
| 免认证时段语法 | `free_windows = 23:30-06:30` | 同一解析语义 |
| 响应词表 | `[response]` 四组（+`hijack_marks`） | 默认值与 Python `RESPONSE_DEFAULTS` 逐字同源 |

不共用的只有**日志文件名**：Python `keeper.log`，Go `cnalw.log`（同目录，
互不覆盖）。Go 另写一个 `api.json`（127.0.0.1 端口 + 随机 token，供
`cnal status` 只读查询；不含任何用户凭据，退出即删）。

## 2. 迁移步骤（约 2 分钟）

```powershell
# 1) 记录现状（可选）：确认 Python 守护在线，备份配置
Copy-Item "$env:LOCALAPPDATA\campus-net-autologin\config.ini" `
          "$env:LOCALAPPDATA\campus-net-autologin\config.ini.bak"

# 2) 关 Python 自启 + 退出 Python 守护
#    等效于托盘菜单取消勾选「开机自启」；值名是 Python 版专用的。
Remove-ItemProperty -Path "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run" `
                    -Name "campus-net-autologin" -ErrorAction SilentlyContinue
#    托盘右键退出，或任务管理器结束 campus-net-autologin.exe / pythonw.exe

# 3) 装 Go（三 exe + 开始菜单 + HKCU Run 自启 CampusNetAutoLogin-Go）
powershell -File scripts\install.ps1

# 4) 启动并验证
& "$env:LOCALAPPDATA\Programs\cnal\cnalw.exe"
& "$env:LOCALAPPDATA\Programs\cnal\cnal.exe" status   # 应打印「状态: 在线」等，退出码 0
```

`install.ps1` 只在 `config.ini` 已存在时登记自启；新机先跑 `cnalw` 完成首跑
配置，再重跑脚本或托盘勾选「开机自启」。

若 Python 版脚本路径不同，用它的托盘菜单取消「开机自启」等效；关键是
**`HKCU\...\Run` 下不能同时留着 `campus-net-autologin` 与
`CampusNetAutoLogin-Go` 两个值**（两版值名不同，互不覆盖，会双守护开机）。

验证点：

1. `cnal status` 退出码 0，状态与面板一致；
2. 托盘菜单「打开面板」能看到账号/服务器（凭据已读到）；
3. 状态日志 `cnalw.log` 有 `状态:` 行（新文件，不追加到 `keeper.log`）；
4. 若提示「配置未就绪」进入首跑面板：说明 ini 缺 `server`/`login_path`
   或凭据没读到，见 §3 的差异点。

## 3. 配置差异（迁移时只需看这三类）

| 键 | Python | Go | 迁移动作 |
|---|---|---|---|
| `ssl_verify` | 缺省 `false` | **缺省 `true`（安全默认）** | 自签门户且 ini 没写这一行 → 显式补 `ssl_verify = false`；本机现有配置已显式写 false，无感 |
| `clash_api_url` / `clash_api_secret` | 可选恢复 | **明确不移植** | 保持原值即可，Go 忽略；代理软件自身心跳是通用主路径 |
| `panel_mode` | 无 | `native`（缺省）/ `browser` | 可选：想省 WebView2 内存就写 `panel_mode = browser` |
| `login_form` | 无（`FORM_BASE` 固定字段名） | 登录表单模板 | Go-only：Python 会忽略该键、继续用固定字段名；异校变体只有 Go 能适配 |
| `password`（ini 明文） | keyring 不可用时的降级路径 | **不读明文**（安全设计） | Python 曾降级明文的机器，首次启动会要求重填一次密码，填后进凭据管理器 |

其余键（`interval` / `probe_timeout` / `login_timeout` / `probe_ips` /
`campus_dns` / `login_page` / `login_path` / `flux_path` / `user_agent` /
`portal_down_wait` / `reject_backoff` / `transient_backoff` / `dual_stack` /
`v6_peer` / `peer_opr` / `peer_path` / `bind_ip` / `auto_reconnect`）同名同义，
Go 直接吃原 ini。

## 4. 回滚（同样可逆）

```powershell
# 1) 退出 Go 托盘（菜单「退出」，让端点文件/锁正常释放）
# 2) 关 Go 自启
powershell -File scripts\remove-autostart.ps1

# 3) 启动 Python 版，再在托盘菜单勾选「开机自启」
& D:\workspace\campus-net-autologin\dist\campus-net-autologin\campus-net-autologin.exe
```

回滚不碰凭据、不还原配置：Go 面板/CLI 只改写 ini 白名单键，格式仍是标准
ini，Python 能直接读。只有两种情况建议恢复备份 `config.ini.bak`：
Go 面板里改过高级参数后想整体退回；或升级前配置来自更老版本。

残留文件说明：强杀 Go 进程会留下 `api.json`，`cnal status` 会报「守护未响应」
（不误报在线）；下次启动自动覆盖，也可手动删除。`cnalw.log` 可保留作历史。

## 5. 排障

| 现象 | 原因 | 处理 |
|---|---|---|
| `cnal status` 退 7「守护未运行」 | 没启动/端点文件被删/端口已关 | 启动 `cnalw.exe`；强杀残留时重启一次即可 |
| 启动弹「配置未就绪」首跑面板 | ini 缺 `server`/`login_path`，或凭据读不到 | 面板填账号密码与门户地址；或先 `cnal detect` 探测 |
| 登录总被判定失败 | 异校响应词表不同 | `cnal compat "<响应>"` 离线判定，再在面板高级区/`[response]` 改词表 |
| 登录请求发出但门户不认 | 异校登录字段名不同（同属 ac_portal 族） | 面板高级区「登录表单」或 ini `login_form` 改字段名，`{user}`/`{pass}` 占位 |
| 证书错误 | 门户自签且缺 `ssl_verify = false` | 显式写 false（仅自签门户；默认必须校验） |
| 开机后两个守护都在 | Python 与 Go 自启项同时存在 | 删掉不需要的那个 Run 值，只留一个 |

## 6. 禁止事项

- **不要并行运行两版**：Go 的单实例锁只锁 Go 自己，Python 的锁也只锁自己，
  两把锁互不感知；双守护会互相踢线并触发门户反暴力冻结。
- 不要手工编辑 `api.json`：token 每次启动随机，改错只会让 `cnal status` 报错。
- 不要把密码写进 ini：Go 不读明文密码，且违反项目凭据纪律。
