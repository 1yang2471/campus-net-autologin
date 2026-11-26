---
name: 门户兼容性报告
about: 报告你学校的适用性判定（零凭据、可公开，帮助更新 README「实测名单」）
labels: compatibility
---

<!-- 请勿粘贴真实账号/密码。--detect 与响应判定均为离线、零凭据设计。 -->

## 环境

- 学校（可选）：
- 门户地址（可脱敏）：
- Windows 版本：
- 网卡形态（WiFi / 有线 / 双网卡同时在线）：
- DHCP 租约时长（`ipconfig /all` 里 Lease Expires − Lease Obtained；短租约≈频繁换 IP）：
- Windows 联网指示（同时双网卡时是否误报「无 Internet」）：是 / 否 / 不适用
- 本机是否装过代理/加速器（Clash 等，仅写有没有）：
- 代理模式（系统代理 / TUN / WFP 或游戏加速器 / 不确定）：
- 面板「门户路径」中的绑定诊断（接口绑定 / 降级：仅绑源 / 接口绑定失败）：
- `/state` 的 `bind_mode` / `bind_interface` / `bind_index` / `bind_degraded`（可脱敏）：

## 1. 协议指纹（先判族，不联网）

```bash
cnal compat --detect "<登录页 HTML 或登录 POST 抓包片段>"
```

输出（粘贴完整）：

```
（粘贴 --detect 输出）
```

## 2. 响应判定（验证词表）

```bash
登录后马上抓一次登录响应文本，再跑：
cnal compat "<登录响应文本>"
```

输出（粘贴完整）：

```
（粘贴 cnal compat 输出）
```

## 3. 结果

- --detect 结论：compatible / variant_unsupported / other_protocol / unknown
- 登录实测：✅ 能自动重连 / ❌ 不能（现象：）
- 是否同意列入 README「实测名单」：是 / 否
「实测名单」随本类 issue 更新；请勿在 issue 里粘贴任何真实凭据。
