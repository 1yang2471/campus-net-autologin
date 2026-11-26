# go-cnal 特有坑：实测故事全文

> 本文由 2026-11-11 tidy 瘦身从 `AGENTS.md` 原样迁出；2026-11-26 追加接口级绑网与 Clash 联动更正。
> `AGENTS.md` 只留「规则 → 由什么拦住」表和一行路标；需要背景时读本文。
> **历史说明（2026-11-20）**：Python 版已退役，文中对 `keeper.py` 等文件的引用
> 保留为历史对照，不再对应仓库内现存文件。

## 特有坑（0–11）

## 特有坑（踩过的，别再踩）

0. **Clash API 联动只做 fake-ip flush**（2026-11-26 起）：仅允许本机回环 HTTP，
   登录/补绑成功后 `POST /cache/fakeip/flush`，2 秒超时，失败只告警不阻断认证。
   不要扩到非回环地址、secret 日志或 `/connections` 全断接口。

1. **物理栈纪律**：Windows 必须同时绑源 IP 与接口索引——单独 `LocalAddr`
   不能阻止路由型 TUN 接管 socket。HTTP、DNS、TLS 探测和 Bindable 共用同一
   Control；严格模式失败即 fail-closed。这就是 `internal/netstack` 存在的理由。
2. **选源只用 `DiscoverSource`（v4）/ `PhysicalSourceV6`（v6）**，不要枚举取第一个：
   2026-11-07 实测旧法选中 FlClash TUN 的 `28.0.0.1`，新法选中物理 Wi-Fi。两条
   契约测试守着它（含"未选中虚拟网卡地址"回归守卫）。
3. **移植 Python 语义时不要砍防风暴闸**。同一个错犯过两次（2026-11-07）：
   G4 砍掉登录节流 → 5.5 分钟 8 次真实登录；G3 砍掉 `SESSION_FALSE` 自停闸 →
   flux 假判死变登录风暴。两次都是真机实测抓回来的。Python 侧对应实现见
   `../campus-net-autologin/keeper.py` 的 tick/handle_rejection/_probe_session。
4. **凭据只从 Windows 凭据管理器读**（`internal/store`，与 Python keyring 同存储）；
   任何文件不得出现真实凭据（服务名 `campus-net-autologin`）。
5. **自签门户才跳过证书校验**：`ssl_verify=false` 是用户显式配置，默认必须校验。
6. **词表只从 api.Label/api.Color 取，装配层不自抄**。2026-11-07 实证：app.Snapshot
   闭包漏填 Label/Color → 托盘拿到空色值 → 图标解析报错被 `if err == nil` 静默跳过
   → 托盘项存在但图标空白（%TEMP% 下无 systray_temp_icon_* 文件是硬证据）。
   判据：任何 `err == nil` 分支都要留一条可见日志，否则失败是无声的。
7. **cnalw 必须 GUI 子系统**：裸 `go build` 产出 console exe → 点开始菜单弹黑终端窗。
   构建走 `scripts/build.ps1`（带 `-H windowsgui`）；`scripts/install.ps1` 复制前会验
   PE 子系统字节，非 2=GUI 直接拒装（护栏经变异测试：翻子系统 3 → 拦截）。
8. **托盘图标可见性有量化判据**：空闲态用 `lamp`（环+心点）不用小实心点——6px 灰点
   缩到 16px 托盘仅 ~3px 肉眼不可见（覆盖率判据见 `TestIdleIconVisibleAtTraySize`，
   ≥25%）。形状词汇唯一出处 `DESIGN.md`，代码/面板 HTML 都从它取，不各自定形状。
9. **门户必须钉真实 IP，不能直接用域名**：系统 DNS 在 TUN 下给 fake-ip
   （2026-11-08 真机实证 `a.stu.edu.cn → 28.0.0.17`），绑物理源发往 fake-ip
   必然超时——登录/补绑/flux 全部打不到门户。组合根用 `portalTarget`（校园 DNS
   绑源解析 + TTL 缓存）+ `PostPinned`/`Get(ipPin)`，解析失败返回 nil 不猜地址
   （Python `_portal_target` / P1-13 同源）。`ResolveBound` 的 campus_dns 裸 IP
   必须经 `normalizeDNSServer` 补 :53，否则报 `missing port` 静默降级；严格接口
   模式只接受 IP 字面量，不接受需系统解析的 DNS 主机名。
10. **登录 POST 带 UA + Referer**：Python `_headers` 同源；裸 Go 客户端会被 WAF
    慢回复/挂起。超时独立 `login_timeout`（0=自适应 max(6s,2×probe)），不复用
    `probe_timeout`（RADIUS 后端 3-6s 才回，Python P1-7 实测结论）。CLI 也必须
    吃 ini 的 `user_agent`/`login_page`；缺省 UA 唯一出处在
    `protocol.DefaultUserAgent`（不许硬编码第二份）。
11. **单实例锁是 OS 建议锁不是"文件存在"**：`app.AcquireInstanceLock`（LockFileEx
   字节锁），cnalw 与 cnald 共用一把；进程死自动释放，强杀不留残留（Python
   2026-11-04 踩过文件存在判断的坑）。两个守护并行会互抢登录撞门户限频。

## 特有坑（12–21）

12. **发布审查实证坑（2026-11-08 trouble-maker 25 项）**：① 传给 `Get/PostPinned`
    的 ipPin 必须 `host:port`（裸 IP 报 missing port → L2 恒报"出口受限"）；
    ② 登录应答 `success:false`+「已在线」是成功哨兵（Python parse_login 同源），
    漏判会让 SESSION_FALSE 自停闸不可达；③ CLI 与守护必须吃同一份 `[keeper]`
    ini、同样钉 IP/带 UA；④ 展示字段 State/Detail/V6State/SessionDead/FreeMode
    全部经 `Daemon.View()` 加锁读（Tick 单写者，其余 goroutine 只能读快照）；
    ⑤ 空密码不得进 `WriteCredential`（`&blob[0]` panic 且覆盖真密码）。
13. **ini 行尾统一按 `\n` 切**（`readIniLines`），`iniEOL` 保留原风格；按 `\r\n`
    切会让 LF 文件整份变一行，面板改一次设置就毁配置。
14. **portalTarget TTL 过期失败 fail-closed**（返回 nil 不返回陈旧 IP），换网后
    不把学号密码发往可能已回收的旧地址。
15. **响应词表唯一出处 `keeper.ResponseMarks`**：默认值与 Python
    `RESPONSE_DEFAULTS` 逐字同源（`TestDefaultMarksMatchPython` 守着），
    异校走 ini `[response]`。不要在 app/api/CLI 里另抄一份成功/失败词；
    `JudgeLoginResponse` 是守护与 CLI 共用的判定入口。
16. **首跑探测全程无凭据**：`cnal detect` 只做无凭据 GET；302 Location 或
    200 正文里的绝对 URL 都必须过 `parsePortalLocation` 安全边界，并再拉
    登录页验 `ac_portal`/`pwdLogin` 指纹才返回。相对链接会落到锚点 IP 上，
    不可信。缺配置/凭据时 cnalw 进首跑面板（`SetupError`），不报错退出。
17. **高级配置按节写入**：`[response]` 四组词表必须落 `[response]` 段
    （`updateIniSection`），数值/文本键落 `[keeper]`；`updateIniFields` 的
    白名单复用 `advancedKeeperKeys`/`advancedTextKeys`，不许另抄一份（漏键
    会让面板保存「假成功」）。值里含换行一律拒绝（防注入新节）。
18. **`cnal compat` 与守护共用判定链**：`CompatCheck` 调 `JudgeLoginResponse`
    + `ResponseMarks`，CLI 的 `[response]` 走 `loadConfig` 同一份 ini；
    禁止在 CLI 里另写成功/失败词表。协议指纹的 Srun 新版端点
    （`get_challenge`/`rad_user_info`）已补，别退回只认 "srun" 字样。
19. **面板打开方式缺省 native**：`panel_mode` 只认 `browser`，其余（含非法值）
    回落 native——坏配置不得改变既有行为；native 失败仍回落浏览器。
20. **`cnal status` 只读、不猜端点**：状态唯一真源是运行中守护的回路
    （`app.RuntimeFilePath()` 写的 `api.json`，含 127.0.0.1 端口 + 随机 token）；
    文件缺失/损坏/请求失败一律报「未运行/未响应」并退 7，不得自己重新探测或
    触发登录。端点文件只落 `%LOCALAPPDATA%`（不含任何用户凭据），守护退出时
    删除；崩溃残留由 `/state` 请求失败兜底，不误报。
21. **登录表单模板唯一出处 `protocol.Form`**：默认模板
    `DefaultFormTemplate` 与 Python `FORM_BASE`/`parse_login` 逐字同形；
    模板必须恰好一个 `{user}` 与一个 `{pass}`，`ipv4or6` 是保留键（双栈统一
    管理）。守护自动登录、v6 补绑、CLI rebind 三条路径都必须传 `Form`
    （漏一条就退回默认字段名）；保存前 `ParseForm` 校验，非法模板不得落 ini。
