# 各地校园网 Web 认证机制调研（2026-11-08）

> 目的：为 go-cnal 的「通用性」三件 P0（响应词表、退避可调、首跑探测）找事实
> 依据，避免把参考校行为当普适规律。调研只做公开资料，**不涉及任何真实凭据**。
> 结论按证据强弱标注；无公开来源的条目单列在 §5，不当事实使用。

## 1. 协议族：校园网认证是割裂市场

| 族（厂商） | 端点/表单特征 | 响应特征 | 本项目适用性 |
|---|---|---|---|
| **ac_portal**（华为 AC 系 Portal） | `GET /ac_portal/default/pc.html`；`POST /ac_portal/login.php`，表单 `opr=pwdLogin&userName=&pwd=&rememberPwd=` | `success` 布尔 + `msg`；已在线如「用户已在线，不需要再次认证」 | **本项目目标族**；明文单 POST，可自动化 |
| **深澜 Srun 新版** | `/srun_portal_pc` → `/cgi-bin/get_challenge` → `/cgi-bin/srun_portal` | `{"error":"ok","ecode":0,"suc_msg":"login_ok"}`；重复登录 `ip_already_online_error` | 不支持：多步 + challenge/密码加密 |
| **深澜旧版** | `POST /srun_portal_pc.php` | 纯文本 `login_ok` | 协议不同，不支持 |
| **Dr.COM / 城市热点 eportal** | `POST /eportal/portal/login`；旧版 `/drcom/login?...` | `callback[{result:0,ret_code:0,msg:'登录成功'}]` | 不支持：`result` 语义随部署反转 |
| **锐捷 ePortal** | `POST /eportal/InterFace.do?method=login` | `{result:"success"}` 或 `dr1003({result:"0"|"1"})` | 不支持：字段/流程不同 |

关键事实：**`result` 的 0/1 语义会随学校反转**（有的学校 `result:"1"`=成功，
有的 `"0"`=成功），不能做全局数值判据。这也是本项目只做 ac_portal 族、
不去猜别家协议的原因。

## 2. 响应文案与判定词（有公开来源的真实词汇）

| 语义 | 见到的词 | 对本项目的含义 |
|---|---|---|
| 成功 | `success:true`、`login_ok`、`ret_code=0`、`msg:"登录成功"` | 现有默认 success 词表覆盖；`ret_code` 判定已补 |
| 已在线 | 「用户已在线，不需要再次认证」、`ret_code=2`、`ip_already_online_error` | 「已在线」必须视为成功哨兵（防误判死→登录风暴） |
| 密码/账号错误 | 「密码不匹配」「账号或密码错误」「用户不存在」「UserName-Err」 | 硬拒绝，长退避 |
| 超限/欠费/停机 | 「达到同时在线用户数量上限」「用户已过期」「认证请求被拒绝」 | 硬拒绝，长退避 |
| 锁定/限频 | `User locked`（5 分钟以上）、连续密码错 10 次进黑名单、参数错禁登 3–5 分钟 | 冻结闸必须独立于普通退避 |

## 3. captive portal 劫持与探测

- **HTTPS 不触发门户**：国科大明确「通过 http 访问会自动弹出认证界面，
  https 不会」。首跑探测必须用 HTTP。
- **302 Location** 是常见自报方式（Dr.COM 未认证访问 `http://1.1.1.1/` 返回
  `Location: http://192.168.100.200`），也是本项目 `cnal detect` 的主路径。
- **200 HTML 劫持页**同样存在（北航网关页用 `<meta http-equiv="refresh">`；
  部分门户直接回登录页）。因此本项目补了 200 正文兜底：只认正文里明确含
  `ac_portal` 的绝对 URL，且仍要指纹复核——相对链接会落到锚点 IP 上，不可信。
- `generate_204` 探测在未认证时会被门户截成 302/200；204 空 body 才是真在线。
- **未找到「DNS 劫持到门户」的可靠公开来源**：不作为设计假设。

## 4. 会话、退避与夜间断网

- 空闲超时无统一值：有学校 2 小时无流量断开，也有 24 小时自动注销；未找到
  4h/12h 的公开依据。
- 夜间措施公开事实是**断网/服务时间窗**（如 24:00 断网、00:07 断 IPv4、
  00:00–05:59 暂停），并非「夜间免认证」。本项目的 `free_windows`（窗口内
  不自动登录）与免会话自动检测（锚点绿+登录不受理）正好覆盖两种现实形态。
- 退避证据：锁定 5 分钟以上、参数错禁登 3–5 分钟、连续失败 10 次黑名单。
  默认 `reject_backoff=600s` 处于同一量级；`transient_backoff=30s` 只用于
  瞬态错误快速重试，连续 5 次升级为长退避。

## 5. 未找到可靠来源（不写成产品承诺）

- 冻结文案「剩余 N s」：目前只有本项目参考校实测语料，无公开 URL。
- 4h/12h 会话超时：无公开校方资料。
- 「夜间免认证」普遍性：无；公开资料反而支持夜间断网更常见。
- DNS 劫持作为主要门户劫持方式：无可靠来源。
- ac_portal 归属华为的官方文档：仅部署路径/参数与二手资料佐证。

## 6. 落到代码的四条设计启示

1. **词表按方言分表，不设全局 `result` 数值判据**；默认词表保持与 Python
   `RESPONSE_DEFAULTS` 逐字同源（参考校零改动），异校在 `[response]` 覆盖。
2. **退避可配**：默认 600/30，范围按 Python `_num` 钳制；冻结闸下限取
   `reject_backoff`，文案剩余秒数更大时以它为准。
3. **首跑探测**：`GET http://<锚点>/generate_204` 不跟随跳转 → 302 Location
   或 200 正文绝对 URL → `ac_portal`/`pwdLogin` 指纹复核；全程无凭据 GET。
   HTTPS 不作为触发依据；门户域名优先用校园 DNS 绑源解析并钉 IP。
4. **登录表单形状可配**：ac_portal 族的端点路径可配，但字段名未必与参考校
   一致；用 `login_form` 模板（静态键值 + `{user}`/`{pass}` 占位）覆盖字段名/
   操作名/静态参数，不引入任意表达式 DSL。默认模板与参考校逐字同形。

## 7. 主要来源

- ac_portal 表单/端点：<https://github.com/victorwoo/Auth-Sinfor/blob/master/Auth-Sinfor.ps1>
- ac_portal 页面路径：<https://github.com/HEX9CF/CupidConnectorLite/blob/main/README.md>
- 深澜 Srun 新版协议：<https://blog.sleepstars.net/archives/shen-da-xiao-yuan-wang-linux-zi-dong-ren-zheng-srun-xie-yi-jie-xi-yu-python-shi-xian>
- Dr.COM eportal：<https://blog.ataw.top/archives/drcom-auto-login>
- 锐捷 ePortal：<https://github.com/GuoYingLong/eportal-login/blob/master/login-eportal.py>
- `result` 语义反转：<https://github.com/leyuwei/SeuWebHelper/blob/master/seuhelper.py>、<https://blog.tuf3i.cc/posts/acp-campus-net/>
- HTTPS 不弹门户：<https://inc.ucas.ac.cn/index.php/zh/xyws/yxwe>
- 200 meta refresh 劫持页：<https://github.com/BUAASubnet/srun>
- generate_204 探测：<https://github.com/Chocola-X/GXU-Net-AutoLogin>
- 夜间断网普遍性：<https://news.cctv.com/2024/05/24/ARTIgitFGjn7ni3kfrOQqoNb240524.shtml>
- 锁定/黑名单：<https://ic.just.edu.cn/2021/0828/c9806a293207/page.htm>、
  <https://xjzx.hebau.edu.cn/info/1183/1928.htm>
