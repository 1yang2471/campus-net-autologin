package app

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// 首跑向导接线的验收：指纹失败后，凭据填好保存配置时触发行为确认——
// 成功继续保存、失败给出 [response] 证据且不阻断手填（保持原有失败处理）。
// confirmFunc 是测试缝，替身不发真实凭据、不触网。

func setupConfirmFixture(t *testing.T, confirm func(daemonConfig, string, string) (bool, string, error)) *Setup {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.ini")
	if err := ensureIniFile(path); err != nil {
		t.Fatal(err)
	}
	old := cfgPathCached
	cfgPathCached = path
	t.Cleanup(func() { cfgPathCached = old })
	// 真实向导 NewSetup 会补 login_path 默认；测试夹具照做，避免 loadDaemonConfig
	// 因缺端点路径返回部分配置（行为确认走的是完整配置路径）。
	if err := updateIniFields(map[string]string{"login_path": "/ac_portal/login.php"}); err != nil {
		t.Fatal(err)
	}

	s := &Setup{cfgPath: path, NeedConfirm: true}
	s.withAPI()
	s.confirmFunc = confirm
	return s
}

// (a) 行为确认成功：配置继续保存，证据说明命中成功词。
func TestSetupSaveConfigConfirmSuccess(t *testing.T) {
	var gotCfg daemonConfig
	var gotUser, gotPass string
	s := setupConfirmFixture(t, func(cfg daemonConfig, user, pass string) (bool, string, error) {
		gotCfg, gotUser, gotPass = cfg, user, pass
		return true, "行为探测命中成功词表（含已在线语义），门户可用", nil
	})
	s.pendingUser, s.pendingPass = "u", "p"

	if err := s.API.SaveConfig(map[string]string{"server": "http://10.0.0.1"}); err != nil {
		t.Fatalf("行为确认成功后应继续保存: %v", err)
	}
	if gotUser != "u" || gotPass != "p" {
		t.Errorf("应把面板填写的凭据交给行为确认，实际 user=%q pass=%q", gotUser, gotPass)
	}
	if gotCfg.Server != "http://10.0.0.1" {
		t.Errorf("确认应读到本次提交（或当前）的 server，实际 %q", gotCfg.Server)
	}
	if !strings.Contains(s.ConfirmEvidence, "成功") {
		t.Errorf("成功应留下正向证据，实际 %q", s.ConfirmEvidence)
	}
	cfg, err := loadDaemonConfig(s.cfgPath)
	if err != nil || cfg.Server != "http://10.0.0.1" {
		t.Errorf("确认成功后 server 应落盘，cfg=%v err=%v", cfg.Server, err)
	}
}

// (b) 行为确认判负：给出 [response] 证据，且保持原有失败处理——仍允许保存
// （面板手填路径不被阻断），证据经状态卡 detail 回显给用户。
func TestSetupSaveConfigConfirmRejectedShowsEvidence(t *testing.T) {
	s := setupConfirmFixture(t, func(daemonConfig, string, string) (bool, string, error) {
		return false, "行为探测被判负（密码错误）", nil
	})
	s.pendingUser, s.pendingPass = "u", "p"

	err := s.API.SaveConfig(map[string]string{"server": "http://10.0.0.1"})
	if err != nil {
		t.Errorf("判负应保持原有失败处理（不阻断手填保存），实际 %v", err)
	}
	for _, want := range []string{"词表可能不匹配", "[response]"} {
		if !strings.Contains(s.ConfirmEvidence, want) {
			t.Errorf("证据应含 %q，实际 %q", want, s.ConfirmEvidence)
		}
	}
	snap := s.API.Snapshot()
	if !strings.Contains(snap.Detail, "[response]") {
		t.Errorf("状态卡 detail 应回显证据，实际 %q", snap.Detail)
	}
	cfg, _ := loadDaemonConfig(s.cfgPath)
	if cfg.Server != "http://10.0.0.1" {
		t.Errorf("原有手填保存路径应保持，server 应落盘，实际 %q", cfg.Server)
	}
}

// (c) 发包系统错误：证据带出根因，不伪造可用性结论，也不阻断保存。
func TestSetupSaveConfigConfirmSendError(t *testing.T) {
	s := setupConfirmFixture(t, func(daemonConfig, string, string) (bool, string, error) {
		return false, "", errors.New("dial tcp: 网络不可达")
	})
	s.pendingUser, s.pendingPass = "u", "p"

	if err := s.API.SaveConfig(map[string]string{"server": "http://10.0.0.1"}); err != nil {
		t.Errorf("发包失败应保持原有失败处理，实际 %v", err)
	}
	if !strings.Contains(s.ConfirmEvidence, "网络不可达") {
		t.Errorf("证据应带出系统错误根因，实际 %q", s.ConfirmEvidence)
	}
}
