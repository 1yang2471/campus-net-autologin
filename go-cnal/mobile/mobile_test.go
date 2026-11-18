package mobile

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"cnal/internal/app"
)

// 桥的验收点：Kotlin 拿到的 JSON 契约不能漂，Start 真的装配内核并给出可
// 加载的面板 URL。测试不打真实门户（interval 设 1h），但 Compose/Listen
// 是真的，不是替身。

func resetState(t *testing.T) {
	t.Helper()
	Stop()
	_ = SetPhysicalSources("", "")
	SetPanelPage("")
}

func TestStateJSONWhenNotRunning(t *testing.T) {
	resetState(t)
	var s struct {
		State  string `json:"state"`
		Detail string `json:"detail"`
		Label  string `json:"label"`
		Color  string `json:"color"`
	}
	if err := json.Unmarshal([]byte(StateJSON()), &s); err != nil {
		t.Fatalf("StateJSON 不是合法 JSON: %v", err)
	}
	if s.State != "starting" || s.Label == "" || s.Color == "" {
		t.Fatalf("未启动时状态快照字段缺失: %+v", s)
	}
	if got := LogsJSON(); !strings.HasPrefix(got, "[") {
		t.Fatalf("LogsJSON 应是 JSON 数组，实际 %q", got)
	}
	for _, fn := range []func() error{
		Login, RebindV6, func() error { return SwitchAccount("u") },
	} {
		if err := fn(); err == nil {
			t.Error("未启动时动作类方法必须报错，不能静默成功")
		}
	}
	if _, err := FluxJSON(); err == nil {
		t.Error("未启动时 FluxJSON 必须报错")
	}
}

func TestSetCredentialsFeedsInjectedSource(t *testing.T) {
	resetState(t)
	t.Cleanup(func() { resetState(t) })
	if err := SetCredentials("alice", "pw-a"); err != nil {
		t.Fatal(err)
	}
	if p, err := creds.Load("alice"); err != nil || p != "pw-a" {
		t.Fatalf("注入的凭据读不回来：p=%q err=%v", p, err)
	}
	if u, p, err := creds.LoadLegacy(); err != nil || u != "alice" || p != "pw-a" {
		t.Fatalf("单条回落未生效：u=%q p=%q err=%v", u, p, err)
	}
	if err := SetCredentials("", "x"); err == nil {
		t.Error("空账号必须拒绝（防止用空串覆盖当前账号）")
	}
	if err := SetCredentials("bob", ""); err == nil {
		t.Error("空密码必须拒绝（防止覆盖已存真密码）")
	}
}

func TestSetPhysicalSourcesValidatesInput(t *testing.T) {
	resetState(t)
	t.Cleanup(func() { resetState(t) })
	if err := SetPhysicalSources("not-an-ip", ""); err == nil {
		t.Error("非法 v4 必须报错")
	}
	if err := SetPhysicalSources("10.0.0.5", "fe80::1"); err == nil {
		t.Error("链路本地 v6（非全局 GUA）必须拒绝——M0 实测模拟器只有 fe80::")
	}
	if err := SetPhysicalSources("2001:db8::1", ""); err == nil {
		t.Error("v6 形态的地址不能当 v4 用")
	}
	if err := SetPhysicalSources("10.0.0.5", "2001:db8::1"); err != nil {
		t.Fatalf("合法的 v4+GUA v6 应通过: %v", err)
	}
}

func TestStartAssemblesKernelAndReturnsPanelURL(t *testing.T) {
	resetState(t)
	t.Cleanup(func() { resetState(t) })

	dir := t.TempDir()
	SetDataDir(dir)
	t.Cleanup(func() { app.SetDataDir("") })
	if err := SetCredentials("alice", "pw-a"); err != nil {
		t.Fatal(err)
	}
	// 注入物理源：host 测试机的网卡与校园网无关，注入固定值让 Compose
	// 不依赖真实网卡发现（同时验证注入缝在 mobile 全链路真的生效）。
	if err := SetPhysicalSources("10.20.30.40", ""); err != nil {
		t.Fatal(err)
	}
	ini := filepath.Join(dir, "config.ini")
	// interval=3600：测试期间 tick 不触发，绝不打真实门户。
	body := "[keeper]\nserver = http://portal.example\nlogin_path = /ac_portal/login.php\n" +
		"username = alice\ninterval = 3600\nprobe_timeout = 1\n"
	if err := os.WriteFile(ini, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	SetPanelPage("<html>panel</html>")

	panelURL, err := Start(ini)
	if err != nil {
		t.Fatalf("Start 应能装配内核：%v", err)
	}
	u, err := url.Parse(panelURL)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.Path != "/panel" {
		t.Fatalf("PanelURL 形态不对：%q (%v)", panelURL, err)
	}
	if u.Hostname() != "127.0.0.1" {
		t.Errorf("回路 API 必须只绑 127.0.0.1，实际 %q", u.Hostname())
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(u.Query().Get("token")) {
		t.Errorf("token 应为 32 hex：%q", u.Query().Get("token"))
	}
	// 端点文件与状态日志都落在注入目录下（路径缝全链路生效）。
	if _, err := os.Stat(filepath.Join(dir, "api.json")); err != nil {
		t.Errorf("端点文件未落在注入目录: %v", err)
	}
	var s struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal([]byte(StateJSON()), &s); err != nil || s.State == "" {
		t.Errorf("运行中 StateJSON 不可用: %q %v", StateJSON(), err)
	}
	// 重复 Start 必须拒绝（两个守护并行会互抢登录撞门户反暴力冻结）。
	if _, err := Start(ini); err == nil {
		t.Error("重复 Start 必须报错")
	}
	Stop() // 幂等
	Stop()
	if !strings.Contains(StateJSON(), "未启动") {
		t.Error("Stop 后应回到未启动状态")
	}
}
