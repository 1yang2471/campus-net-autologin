package app

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"cnal/internal/keeper"
	"cnal/internal/protocol"
)

// 首跑向导「行为确认」路径的验收（TDD 红灯先行）：
//  1. 指纹快筛命中 → 直接继续，不触发行为确认；
//  2. 指纹失败 + 行为确认命中成功词 → 向导继续（与指纹成功路径同处理）；
//  3. 指纹失败 + 行为确认判负 → 不继续，且证据文案点明 [response] 词表可能不匹配；
//  4. 指纹失败 + 发包系统错误 → 不继续，证据如实带出错误（不伪造可用性结论）。
//
// 判定动作由 confirm 缝注入，测试不发真实凭据、不触网。

// TestSetupConfirmDecisionFingerprintOK 指纹成功时行为确认缝根本不该被调用——
// 行为确认只是快筛失败后的第二路径，不得抢指纹的正常工作流。
func TestSetupConfirmDecisionFingerprintOK(t *testing.T) {
	called := false
	proceed, ev := SetupConfirmDecision(true, func() (bool, string, error) {
		called = true
		return false, "不该被调用", nil
	})
	if !proceed {
		t.Error("指纹快筛命中时向导应继续")
	}
	if called {
		t.Error("指纹命中时不应触发行为确认")
	}
	if ev != "" {
		t.Errorf("指纹命中不应带行为确认证据，实际 %q", ev)
	}
}

// TestSetupConfirmDecisionConfirmSuccess 指纹失败 + 行为确认成功 → 继续。
func TestSetupConfirmDecisionConfirmSuccess(t *testing.T) {
	proceed, ev := SetupConfirmDecision(false, func() (bool, string, error) {
		return true, "行为探测命中成功词表（含已在线语义），门户可用", nil
	})
	if !proceed {
		t.Error("行为确认命中成功词时向导应继续（与指纹成功路径一样）")
	}
	if !strings.Contains(ev, "成功") {
		t.Errorf("证据应说明命中成功词，实际 %q", ev)
	}
}

// TestSetupConfirmDecisionConfirmRejected 指纹失败 + 判负 → 不继续，给出明确证据。
func TestSetupConfirmDecisionConfirmRejected(t *testing.T) {
	proceed, ev := SetupConfirmDecision(false, func() (bool, string, error) {
		return false, "行为探测被判负（密码错误）", nil
	})
	if proceed {
		t.Error("行为确认判负时不应继续保存配置")
	}
	for _, want := range []string{"词表可能不匹配", "[response]"} {
		if !strings.Contains(ev, want) {
			t.Errorf("证据应包含 %q，实际 %q", want, ev)
		}
	}
}

// TestSetupConfirmDecisionSendError 发包系统错误 → 不继续，证据带出根因。
func TestSetupConfirmDecisionSendError(t *testing.T) {
	boom := errors.New("dial tcp: 网络不可达")
	proceed, ev := SetupConfirmDecision(false, func() (bool, string, error) {
		return false, "", boom
	})
	if proceed {
		t.Error("发包失败时不应继续")
	}
	if !strings.Contains(ev, boom.Error()) {
		t.Errorf("证据应带出系统错误根因，实际 %q", ev)
	}
}

// TestSetupConfirmDecisionNoSeam 未接行为确认缝 → 退回旧行为（不自动继续，
// 面板允许手填），不得 panic。
func TestSetupConfirmDecisionNoSeam(t *testing.T) {
	proceed, ev := SetupConfirmDecision(false, nil)
	if proceed {
		t.Error("未接行为确认缝时不应自动继续")
	}
	if ev == "" {
		t.Error("未接缝时应给出可操作的提示")
	}
}

// TestConfirmSetupPortalUsesCfgFormAndMarks 生产接线：ConfirmSetupPortal 必须
// 用 cfg 当前的 LoginForm（字段名/方法/静态参数）与 Response 词表，以及传入的
// 凭据填表——异校配置改 ini 即生效，不在代码里硬编码参考校表单。
func TestConfirmSetupPortalUsesCfgFormAndMarks(t *testing.T) {
	got := make(chan capturedDelivery, 1)
	p, srv := confirmSendFixture(t, "WELCOME", func(c *capturedDelivery) { got <- *c })
	cfg := daemonConfig{
		Server: srv.URL, LoginPath: "/login", LoginPage: "/", UserAgent: "test-agent",
		// 异校 GET 型表单：字段名不同、带静态参数，成功词也自定义。
		LoginForm: protocol.Form{Method: "GET"}, // 字段由 ParseForm 构造见下
		Response:  keeper.ResponseMarks{Success: []string{"WELCOME"}},
	}
	form, err := protocol.ParseForm("GET&opr=login&username={user}&password={pass}&app=wifi")
	if err != nil {
		t.Fatal(err)
	}
	cfg.LoginForm = form

	ok, ev, err := ConfirmSetupPortal(cfg, "u", "p", p)
	if err != nil || !ok {
		t.Fatalf("自定义表单 + 自定义成功词应判可用，ok=%v err=%v 证据=%q", ok, err, ev)
	}
	req := <-got
	if req.method != http.MethodGet {
		t.Errorf("应按 cfg.LoginForm.Method 走 GET，实际 %s", req.method)
	}
	if req.query.Get("username") != "u" || req.query.Get("password") != "p" || req.query.Get("app") != "wifi" {
		t.Errorf("应按异校表单字段填值，实际 %v", req.query)
	}
	if !strings.Contains(ev, "成功") {
		t.Errorf("证据应说明命中成功词，实际 %q", ev)
	}
}

// TestConfirmSetupPortalJudgedByMarks 同一响应在自定义负向词下应判负，证明
// 判定链用的是 cfg.Response（[response] 段），不是写死的默认成功词。
func TestConfirmSetupPortalJudgedByMarks(t *testing.T) {
	p, srv := confirmSendFixture(t, "denied", func(*capturedDelivery) {})
	cfg := daemonConfig{
		Server: srv.URL, LoginPath: "/login", LoginPage: "/", UserAgent: "test-agent",
		LoginForm: protocol.DefaultForm(),
		Response:  keeper.ResponseMarks{Negative: []string{"denied"}},
	}
	ok, ev, err := ConfirmSetupPortal(cfg, "u", "p", p)
	if err != nil || ok {
		t.Fatalf("自定义负向词应判不可用，ok=%v err=%v", ok, err)
	}
	if !strings.Contains(ev, "词表可能不匹配") {
		t.Errorf("证据应点明词表可能不匹配，实际 %q", ev)
	}
}
