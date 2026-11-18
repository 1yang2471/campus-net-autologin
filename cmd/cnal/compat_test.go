package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cnal/internal/keeper"
)

// cnal compat：离线自检的 CLI 契约（Python compat_check.py 同款用法）。
// 测试针对可测的渲染/文件读取缝，不依赖真门户、不触网。

func TestCompatReportResponseSuccess(t *testing.T) {
	out := compatReport(`{"success":true,"msg":"ok"}`, false, keeper.DefaultResponseMarks())
	for _, want := range []string{"判定: success", "规则:", "瞬态(可重试): 否"} {
		if !strings.Contains(out, want) {
			t.Errorf("成功报告应含 %q，实际:\n%s", want, out)
		}
	}
}

func TestCompatReportRejectSuggestsResponseMarks(t *testing.T) {
	out := compatReport(`{"success":false,"msg":"密码错误"}`, false, keeper.DefaultResponseMarks())
	for _, want := range []string{"判定: reject", "密码错误", "[response]"} {
		if !strings.Contains(out, want) {
			t.Errorf("拒绝报告应含 %q 并提示改词表，实际:\n%s", want, out)
		}
	}
}

func TestCompatReportDetectFingerprint(t *testing.T) {
	out := compatReport(`<form action="/ac_portal/login.php">opr=pwdLogin</form>`, true, keeper.DefaultResponseMarks())
	for _, want := range []string{"协议族: ac_portal", "结论: compatible", "auth_tag: 无", "建议:"} {
		if !strings.Contains(out, want) {
			t.Errorf("协议指纹报告应含 %q，实际:\n%s", want, out)
		}
	}
}

func TestCompatSampleFromFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "resp.txt")
	if err := os.WriteFile(p, []byte(`{"ret_code":0}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sample, err := compatSample("", p)
	if err != nil {
		t.Fatal(err)
	}
	if sample != `{"ret_code":0}` {
		t.Errorf("--file 应读文件原文，实际 %q", sample)
	}
	if _, err := compatSample("", filepath.Join(t.TempDir(), "missing.txt")); err == nil {
		t.Error("缺文件必须报错")
	}
}

// 回归：Go flag 在第一个位置参数后停止解析，"cnal compat <样本> --config x"
// 会把 --config 当第二个位置参数（Python 手写解析没这个坑）。CLI 必须按
// README 用法吃下位置参数后的 flag。
func TestRunCompatCmdAcceptsFlagAfterSample(t *testing.T) {
	ini := filepath.Join(t.TempDir(), "c.ini")
	if err := os.WriteFile(ini, []byte("[keeper]\nserver = https://x\nlogin_path = /a\n"+
		"[response]\nsuccess_marks = OK\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runCompatCmd([]string{"OK", "--config", ini}); code != exitOK {
		t.Errorf("位置参数后的 --config 应被接受，实际退出码 %d", code)
	}
}

func TestCompatMarksFromIni(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.ini")
	ini := "[keeper]\nserver = https://x\nlogin_path = /a\n" +
		"[response]\nsuccess_marks = OK\ntransient_marks = busy\n"
	if err := os.WriteFile(p, []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	// 异校词表必须经 CLI 同一份 ini 加载链进入 compat，否则自检结果与守护不一致。
	if len(cfg.Response.Success) != 1 || cfg.Response.Success[0] != "OK" {
		t.Errorf("CLI 应读 [response] 的 success_marks，实际 %v", cfg.Response.Success)
	}
	if out := compatReport("OK", false, cfg.Response); !strings.Contains(out, "判定: success") {
		t.Errorf("自定义 success 词应影响自检结论，实际:\n%s", out)
	}
}
