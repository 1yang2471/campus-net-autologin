# Windows 接口级绑网 Implementation Plan — Tasks 7–9

### Task 7: 同步文档、版本与兼容性报告

**Files:**
- Modify: `README.md`
- Modify: `go-cnal/README.md`
- Modify: `docs/faq.md`
- Modify: `.github/ISSUE_TEMPLATE/compat_report.md`
- Modify: `go-cnal/scripts/build.ps1:2`
- Create: `go-cnal/docs/2026-11-26-Windows接口绑网与代理共存根治复盘.md`
- Create: `go-cnal/docs/release-notes-v2.4.2-go.md`
- Modify: `go-cnal/docs/2026-11-18-公开发布评估.md`

- [ ] **Step 1: 更新配置说明与真实边界**

README 写明：

```ini
strict_interface_bind = true
clash_api_url = http://127.0.0.1:9090
clash_api_secret =
```

- `strict_interface_bind=true`：Windows 默认，接口绑定失败不转系统栈、不发凭据。
- `false`：允许降级为仅绑源 IP，状态显示“降级”。
- `net_mode=system` 仍是显式专家覆盖。
- Clash API 仅本机回环，登录成功后 `POST /cache/fakeip/flush`，2 秒超时，失败不阻断认证。
- 接口绑定解决路由型 TUN；WFP 按进程重定向与校园 AC/DPI 仍是外部边界。

- [ ] **Step 2: 删除过时承诺并修正版本**

- 删除“IPv6 未绑物理源”“本版仍仅 Windows”等过时表述。
- 修正 `go-cnal/README.md` 的 Android M0 旧描述，但本期不发布 APK。
- `build.ps1` 默认版本改为 `2.4.2-go`。
- README 版本表增加 `v2.4.2-go` Windows 条目。
- 根 README 超过 20,000 字节前先删除重复/过时段落。

- [ ] **Step 3: 增加复盘与发布评估**

复盘必须包含：11-26 时间线、已确认缺口、实现、测试矩阵、真机 A/B、未覆盖的 WFP/AC 边界、最终资产 SHA-256。历史复盘只追加引用，不改写历史结论。

`release-notes-v2.4.2-go.md` 包含：变更摘要、配置迁移、验证结果、已知边界、Windows zip 安装方法；未完成的安全扫描或真机项如实标注。

- [ ] **Step 4: 检查文档门**

Run: `bash go-cnal/scripts/check-file-size.sh`

Expected: PASS；根 README 与所有文本文件小于 20KB。

- [ ] **Step 5: 提交**

```bash
git add README.md go-cnal/README.md docs/faq.md .github/ISSUE_TEMPLATE/compat_report.md go-cnal/scripts/build.ps1 go-cnal/docs/2026-11-26-Windows接口绑网与代理共存根治复盘.md go-cnal/docs/release-notes-v2.4.2-go.md go-cnal/docs/2026-11-18-公开发布评估.md
git commit -m "docs(v2.4.2): 同步接口绑网、Clash 联动与发布边界"
```

---

### Task 8: 全量回归、跨平台编译与真机 A/B

**Files:**
- Test: 全部现有测试
- Create: `go-cnal/docs/2026-11-26-Windows接口绑网验证矩阵.md`

- [ ] **Step 1: 全量测试与静态检查**

```bash
go -C go-cnal vet ./...
go -C go-cnal test ./... -count=1
```

Expected: 全绿，无跳过测试。

- [ ] **Step 2: 跨平台编译**

```bash
GOOS=linux GOARCH=amd64 go -C go-cnal build ./...
GOOS=android GOARCH=arm64 CGO_ENABLED=1 go -C go-cnal build ./mobile
```

Expected: PASS；非 Windows 不引用 Windows API，Android 共享包保持可编译。若 Android 缺 NDK，记录环境阻塞，不得假装通过。

- [ ] **Step 3: Windows 真机矩阵**

逐项记录 `/state` 的 `bind_mode`、`bind_interface`、`bind_index`、`bind_degraded`、`route`、`auth_state`、`user_path`：

1. FlClash 关闭，仅有线。
2. FlClash 系统代理开启，TUN 关闭。
3. FlClash TUN 开启，确认 HTTP/DNS 仍报告物理接口。
4. 双网卡同时在线并切换默认路由。
5. 拔插网线、WiFi 重关联、DHCP 同 IP 重连。
6. 休眠/唤醒。
7. 注入接口绑定失败，确认凭据请求为 0。
8. Clash API 未配置、成功、错误地址、非 2xx。

- [ ] **Step 4: 提交验证矩阵**

```bash
git add go-cnal/docs/2026-11-26-Windows接口绑网验证矩阵.md
git commit -m "test(windows): 记录接口绑网真机验证矩阵"
```

---

### Task 9: 构建、安装验收并发布

**Files:**
- Modify: `go-cnal/docs/2026-11-18-公开发布评估.md`
- Artifact: `go-cnal/dist/go-cnal-2.4.2-go-win64.zip`

- [ ] **Step 1: 构建最终 zip**

```powershell
cd D:\workspace\campus-net-autologin\go-cnal
powershell -ExecutionPolicy Bypass -File scripts\build.ps1 -Version 2.4.2-go
```

Expected: `go vet`、`go test` 通过，生成 onedir zip。

- [ ] **Step 2: 安装并运行发布验收**

```powershell
powershell -ExecutionPolicy Bypass -File scripts\install.ps1
python -I scripts\release_verify.py
```

Expected: 状态、设置、日志、流量、单实例锁断言全绿；截图写入 `docs/screenshots/release-*.png`。

- [ ] **Step 3: 记录哈希与安全状态**

```powershell
Get-FileHash dist\go-cnal-2.4.2-go-win64.zip -Algorithm SHA256
```

发布评估记录测试输出、真机矩阵、截图、SHA-256、Mimosa deep 状态。未完成完整扫描时明确写“未完整扫描”，不得宣称通过。

- [ ] **Step 4: 提交发布收尾**

```bash
git add go-cnal/docs/2026-11-18-公开发布评估.md go-cnal/docs/screenshots
git commit -m "release(v2.4.2-go): Windows 接口绑网与 Clash 联动验收"
```

- [ ] **Step 5: 推送、打 tag 并发布**

```bash
git push origin release
git tag -a v2.4.2-go -m "v2.4.2-go: Windows 接口级绑网与 Clash fake-ip 联动"
git push origin v2.4.2-go
gh release create v2.4.2-go go-cnal/dist/go-cnal-2.4.2-go-win64.zip --title "v2.4.2-go" --notes-file go-cnal/docs/release-notes-v2.4.2-go.md
```

Expected: GitHub Release 仅含 Windows zip；tag 指向最终 release commit；资产 digest 与本地一致。

- [ ] **Step 6: 发布后核验**

```bash
gh release view v2.4.2-go --json tagName,targetCommitish,assets
git status --short --branch
```

Expected: tag、资产名、大小、digest 正确；工作区干净。
