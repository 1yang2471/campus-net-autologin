# v2.4.2-go — Windows 接口级绑网与 Clash 联动

## 变更

- Windows 物理栈从“仅绑源 IP”升级为“源 IP + 接口索引”：HTTP、DNS、门户 TLS、Bindable、IPv6 Peer 与多通道登录都在 socket connect/bind 前设置 `IP_UNICAST_IF` / `IPV6_UNICAST_IF`。
- 新增 `strict_interface_bind`，Windows 默认 `true`：接口绑定失败即拒绝，不转系统栈、不发凭据；显式 `false` 才降级为仅绑源 IP。
- 新增仅本机回环的 Clash/Mihomo fake-ip 联动：登录或补绑成功后 `POST /cache/fakeip/flush`，2 秒超时，失败不阻断认证。
- `/state`、面板门户路径与 `cnal status` 显示接口绑定、接口名、索引和降级状态。
- 同步修正 README 中 Android 与 IPv6 的过时描述。

## 配置

```ini
[keeper]
strict_interface_bind = true
net_mode = auto
campus_dns = <校园 DNS IPv4>
clash_api_url =
clash_api_secret =
```

- `campus_dns` 在严格接口模式下必须是 IP 字面量；TUN 环境强烈建议填写。
- `net_mode=system` 是显式专家覆盖，会绕过物理栈。
- `clash_api_url` 仅接受本机回环 HTTP 地址，例如 `http://127.0.0.1:9090`。

## 验证

- `go vet ./...` 通过。
- `go test ./... -count=1` 全包通过。
- Windows socket option 回读、严格零凭据回退、Clash API 安全校验通过。
- Linux 共享内核编译通过；Android AAR 跨平台构建通过。
- Windows 真机 TUN A/B：TUN 开启且默认路由被接管时，守护仍绑定物理 WLAN；FlClash 实时连接列表未出现 `cnalw.exe`。

## 已知边界

- 接口绑定解决路由型 TUN，不保证绕过 WFP 按进程重定向的游戏加速器。
- 校园 AC/DPI 出口限制窗仍无法本地修复；门户在线但出口受限时不要反复注销重登。
- 本期只发布 Windows zip，Android APK 保持 `v2.4.1-go` 资产不变。
- 未重跑 Mimosa deep 完整扫描；本次安全依据为全量测试、静态检查、回环/凭据边界测试和真机 TUN A/B，不宣称取得新的 completed/0 扫描封印。

## 安装

解压 `go-cnal-2.4.2-go-win64.zip`，运行：

```powershell
powershell -ExecutionPolicy Bypass -File scripts\install.ps1
```

用户级安装，不需要管理员权限。
