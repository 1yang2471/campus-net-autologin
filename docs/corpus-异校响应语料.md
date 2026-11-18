# 异校响应语料与覆盖度实测

> 2026-11-09。**这份文档不是异校实测结论**：样本来自公开的一手代码/校方页面/技术博客，
> 逐条带来源与可信度；我们只做「把真实应答回放进判定器、看能不能判对」的离线测量。
> 可执行形式（逐字原文 + 断言）在 `internal/keeper/corpus_replay_test.go`，
> 本页只列来源与结论，避免两边各抄一份正文。

## 0. 为什么这么做

异校真机环境拿不到，但「异校能不能用」这个问题不能一直靠推断。可行的替代是：
把公开资料里**真实的应答形态**变成回放语料，测我们这侧判不判得对。它证明不了
"在对方校园网里能连通"，但能证明"对方的应答我们读不读得懂"——后者正是可配置面
（`[response]` 词表 / `login_form`）存在的意义。

## 1. 覆盖度实测（16 条样本）

| 分类 | 条数 | 含义 |
|---|---:|---|
| 默认词表就够 | **13** | 开箱可用，异校不用动 `[response]` |
| 需要覆盖词表 | **2** | 默认判错，但写一段 `[response]` 就能对（覆盖后已断言必对） |
| 词表覆盖也表达不了 | **1** | 真缺口，要改代码 |
| 本层回放不了（见 §4） | 3 类 | GET 型端点 / 密码加密 / 非 ac_portal 指纹 |

**围栏口径**：语料只回放**判定层** `JudgeLoginResponse`。端点、传输方式、
加密、探测指纹不在这一层，另有缺口（§4）。

## 2. 来源清单（逐条）

`verbatim` = 正文是否逐字引自来源；`false` 表示来源只给了判定逻辑/字段，
形态是按来源的判定字段补的，**不冒充原文**。

| id | 族 | verbatim | 来源 | 可信度 |
|---|---|---|---|---|
| `acp-pseudo-ok` | ac_portal | ✔ | [Linvery/sangfor-ngac-autologon](https://github.com/Linvery/sangfor-ngac-autologon/blob/master/school_netlogin.py) | 一手代码（原文只引前 16 字符） |
| `acp-pseudo-fail` | ac_portal | ✔ | 同上 | 一手代码 |
| `acp-already-online` | ac_portal | ✘ | [victorwoo/Auth-Sinfor.ps1](https://github.com/victorwoo/Auth-Sinfor/blob/master/Auth-Sinfor.ps1) | 一手代码（msg 逐字，外壳补齐） |
| `acp-text-logon-success` | ac_portal | ✔ | [mitsea 博客（搜索快照，原页 404）](https://git.mitsea.com/FlintyLemming/MitseaBlog) | **低**：只拿到摘要 |
| `drcom-result0-ok` | Dr.COM | ✘ | [blog.ataw.top](https://blog.ataw.top/archives/drcom-auto-login) | 博客转述（回调名被工具脱敏，用占位名） |
| `drcom-result1-escaped-ok` | Dr.COM | ✔ | [jb51.net](https://www.jb51.net/article/230647.htm) | 博客转述（逐字，含 `\u` 转义） |
| `drcom-result1-loose-ok` | Dr.COM | ✔ | 同上 | 博客转述（逐字） |
| `rj-success` | 锐捷 eportal | ✘ | [GuoYingLong/eportal-login](https://github.com/GuoYingLong/eportal-login/blob/master/login-eportal.py) | 一手代码（判定依据逐字，成功体未逐字） |
| `rj-bad-password` | 锐捷 eportal | ✔ | 同上 | 一手代码（`message` 逐字） |
| `rj-no-user` | 锐捷 eportal | ✔ | 同上 | 一手代码 |
| `srun-ok` | Srun | ✔ | [blog.sleepstars.net](https://blog.sleepstars.net/archives/shen-da-xiao-yuan-wang-linux-zi-dong-ren-zheng-srun-xie-yi-jie-xi-yu-python-shi-xian) | 博客转述（真机 JSONP 原文） |
| `srun-already-online` | Srun | ✔ | 同上 | 博客转述 |
| `srun-not-online` | Srun | ✔ | [huhu.ciduid.top](https://huhu.ciduid.top/2022/1113/srun-login-analyze/) | 博客转述（真机 JSONP 原文） |
| `just-wrong-password` | 校方文案 | ✔ | [江苏科技大学自助页](http://ic.just.edu.cn/2023/0412/c6175a322383/page.htm) | 校方一手 |
| `just-user-locked` | 校方文案 | ✔ | 同上 | 校方一手 |
| `just-no-account` | 校方文案 | ✔ | 同上 | 校方一手 |

脱敏：IP / MAC / AC 名 / 学号 / token 一律替换为占位符；样本内**不含任何凭据**。

## 3. 三条要动手的结论

### 3.1 需要写 `[response]` 的两所（覆盖后已断言必对）

- **深澜 Srun**：成功体里字段名就叫 `error`（`"error":"ok"`），而默认负向词表含
  裸 `error` —— **成功被判成失败**。这是最危险的方向：守护以为登录失败，可能
  一边重试一边其实早已在线。解法是覆盖词表，**且必须同时收窄负向词**（否则
  `error` 仍先命中，只加 success 词无效）：

  ```ini
  [response]
  success_marks = 成功,登录成功,login_ok
  negative_marks = 未成功,不成功,失败,错误,not_online_error,no_response_data_error
  ```

- **Dr.COM（`result:"1"` + `\u` 转义中文）**：正文里没有「成功」二字（被转义成
  `\u8ba4\u8bc1\u6210\u529f`），只能按结构键认成功。覆盖项较脆，属该校特有：

  ```ini
  [response]
  success_marks = 成功,登录成功,login_ok,"result":"1"
  ```

### 3.2 词表表达不了的一所：Srun「已在线」

`ip_already_online_error` 的语义是**已在线**（重复登录），我们的「已在线」哨兵是
硬编码中文正则（`internal/keeper/sessfalse.go` 的 `reAlreadyOnline`）。词表只能
表达正/负向，**表达不了「这是已在线」**。要支持英文族，得给 already-online 单独
开一个可配置项（预期收益：避免把「已在线」按成功记入而漏掉会话已死判据）。

### 3.3 锐捷 `queryString`：模板语法本身表达不了（不是 `%` 护栏的锅）

先纠正一个可能的误判。锐捷 eportal 的真实登录表里，`queryString` 的值**本身就是
一整段查询串**：

```
queryString=wlanuserip%3D1.2.3.4%26wlanacname%3DAC%26ssid%3DSSID%26nasip%3D…%26mac%3DMAC%26t%3Dwireless-v2%26url%3D
```

（来源：[jishuzhan 河海大学抓包](https://jishuzhan.net/article/1879467936154914817)；
IP/MAC/AC 名已脱敏。）

两条路都被堵死，且**第二条与 `%` 护栏无关**：

1. 写编码形态（`%3D`/`%26`）→ 被 `ParseForm` 的 `%` 护栏拒绝（该护栏是
   2026-11-08 为"devtools 抄来的编码值会被二次编码"加的）；
2. 写未转义原文（含 `&`/`=`）→ 被模板语法切碎：内层 `&` 把 `wlanacname=AC` 等
   泄漏成顶层字段。实测 **期望 8 个字段，实际被切成 14 个**
   （`TestCorpusRuijieQueryStringFormIsNotExpressible`）。

也就是说：**这类「某个字段的值是整段查询串」的门户，当前 `login_form` 表达不了**，
不是词表问题、也不是 `%` 护栏单独造成的。要补的能力是「某字段的值可承载含 `&`/`=`
的原文，并且只编码一次」（引号语法或 per-field raw 前缀）。

## 4. 本层回放不了的三类（读代码确认，非回放）

| 类 | 事实 | 影响 |
|---|---|---|
| **GET 型登录端点** | Dr.COM / Srun 的登录是 GET（参数在 query）。我们的投递只有 `PostPinned`（`internal/app/compose_portal.go`），`login_form` 也只进请求体 | 这两族**当前不可能登录成功**，不是词表能救的 |
| **密码加密** | 深信服变体有 RC4（`rc4Key`/`auth_tag`）、Srun 有 `{MD5}`/`{SRBX1}`。我们按原文发送 | 这类门户需要用适配层，`login_form` 静态模板从设计上承载不了动态加密 |
| **探测指纹** | `detect` 只认 `ac_portal`/`pwdLogin` | eportal/Srun 门户会被判成「未探测到校园门户」，首跑引导走不通 |

## 5. 边界（不要读过头）

- **这不是异校实测**：没有在对方校园网里跑过，证明不了连通性、时延、门户策略
  （按 MAC 绑会话、限频阈值、夜间措施）。
- 样本里 `acp-text-logon-success` 可信度低（只拿到搜索摘要）；`drcom-result0-ok`
  的回调名被工具脱敏，用的是占位名。
- **`result` 语义反转只证到"两个来源不一致"**（一处 `result:0` 成功、一处
  `result:"1"` 成功），没有"同一校内反转"的证据；数值语义无法用词表表达，
  只能按校覆盖。
- 语料的每一行断言都只是"判定层读得懂"，不代表该校能登录。
- Mimosa 本轮仍是部分扫描；本页不构成安全结论。
