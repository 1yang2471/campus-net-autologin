package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cnal/internal/api"
	"cnal/internal/app"
)

// cnal status：读守护启动时写的端点文件 → GET /state（Bearer token）→ 打印。
// 只读回路，不触网、不触发登录。

func TestFetchStatusSendsBearerAndDecodes(t *testing.T) {
	var gotAuth, gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"state":"offline","label":"离线","detail":"未认证","v4_src":"1.2.3.4"}`)
	}))
	defer ts.Close()

	snap, err := fetchStatus(app.RuntimeInfo{URL: ts.URL, Token: "tok"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok" || gotPath != "/state" {
		t.Errorf("应带 Bearer 头请求 /state，实际 auth=%q path=%q", gotAuth, gotPath)
	}
	if snap.State != "offline" || snap.Detail != "未认证" || snap.V4Src != "1.2.3.4" {
		t.Errorf("快照字段未解码，实际 %+v", snap)
	}
}

func TestFetchStatusErrors(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"401 未授权": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		},
		"坏 JSON": func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `not-json`)
		},
	}
	for name, h := range cases {
		ts := httptest.NewServer(h)
		if _, err := fetchStatus(app.RuntimeInfo{URL: ts.URL, Token: "tok"}, time.Second); err == nil {
			t.Errorf("%s 必须报错（不得打印假状态）", name)
		}
		ts.Close()
	}
	// 端口已关：错误必须冒泡，status 据此报「守护未响应」。
	ts := httptest.NewServer(http.NotFoundHandler())
	url := ts.URL
	ts.Close()
	if _, err := fetchStatus(app.RuntimeInfo{URL: url, Token: "tok"}, 500*time.Millisecond); err == nil {
		t.Error("端点不可达必须报错")
	}
}

func TestFormatStatus(t *testing.T) {
	out := formatStatus(api.Snapshot{
		State: "online", Label: "在线", Detail: "会话正常",
		V6State: "ok", V6Label: "IPv6 在线",
		V4Src: "192.0.2.10", V6Src: "2408::1",
		BindMode: "interface", BindInterface: "WLAN 2", BindIndex: 9,
		Extra: map[string]bool{"session_dead": false, "free_mode": false, "auto_reconnect": true},
	})
	for _, want := range []string{
		"状态: 在线", "详情: 会话正常", "v6: IPv6 在线",
		"v4 源: 192.0.2.10", "v6 源: 2408::1", "绑定: interface WLAN 2(#9)",
		"会话: 正常", "免会话检测: 未触发", "自动重连: 开",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("状态输出应含 %q，实际:\n%s", want, out)
		}
	}
}

func TestFormatStatusShowsBindErrorWithoutMode(t *testing.T) {
	out := formatStatus(api.Snapshot{State: "offline", BindError: "接口绑定失败: test"})
	if !strings.Contains(out, "绑定: unknown") || !strings.Contains(out, "接口绑定失败") {
		t.Fatalf("仅错误也应显示绑定诊断，实际:\n%s", out)
	}
}

func TestFormatStatusDegradesHonestly(t *testing.T) {
	// 无 v6 标签/源、无扩展位：不得编造 v6 行或开关状态。
	out := formatStatus(api.Snapshot{State: "portal_down", Detail: "认证服务器不可达"})
	if strings.Contains(out, "v6:") {
		t.Errorf("无 v6 标签时不应输出 v6 行:\n%s", out)
	}
	if !strings.Contains(out, "状态: 认证服务器不可达") {
		t.Errorf("Label 缺省时应用 api.Label 回退，实际:\n%s", out)
	}
	if strings.Contains(out, "自动重连") {
		t.Errorf("扩展位缺失时不应输出开关行:\n%s", out)
	}
	if !strings.Contains(out, "v4 源: （未知）") {
		t.Errorf("源未知时应显式写未知，实际:\n%s", out)
	}
}

// 首拍前 state/label 都是空串：不得打印「状态: 」空行（面板同样回落启动中）。
func TestFormatStatusEmptyStateFallsBackToStarting(t *testing.T) {
	out := formatStatus(api.Snapshot{})
	if !strings.Contains(out, "状态: 启动中") {
		t.Errorf("空状态应回落「启动中」，实际:\n%s", out)
	}
}

func TestStatusReportEndToEnd(t *testing.T) {
	srv := api.New()
	srv.Snapshot = func() api.Snapshot {
		return api.Snapshot{
			State: "online", Detail: "会话正常", V4Src: "172.29.1.2", V6Src: "2408::1",
			Extra: map[string]bool{"session_dead": false, "free_mode": false, "auto_reconnect": true},
		}
	}
	addr, err := srv.Listen()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "api.json")
	cleanup, err := app.WriteRuntimeFile(p, "http://"+addr.String(), srv.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	var out bytes.Buffer
	if code := statusReport(p, &out, 2*time.Second); code != exitOK {
		t.Fatalf("运行中守护应返回 exitOK，实际 %d", code)
	}
	if !strings.Contains(out.String(), "状态: 在线") {
		t.Errorf("端到端输出应含状态，实际:\n%s", out.String())
	}
}

func TestStatusReportNoDaemon(t *testing.T) {
	var out bytes.Buffer
	code := statusReport(filepath.Join(t.TempDir(), "missing.json"), &out, time.Second)
	if code != exitNoDaemon {
		t.Errorf("无端点文件应返回 exitNoDaemon，实际 %d", code)
	}
	if out.Len() != 0 {
		t.Errorf("未拿到快照时不得打印状态，实际:\n%s", out.String())
	}
}

// dispatch 路由回归：status 与 --status 两种写法都必须进 status 命令
// （无端点文件时返回 exitNoDaemon=7，而不是 usage 的 exitCfg=2）。
func TestDispatchStatusAliases(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing.json")
	for _, args := range [][]string{
		{"status", "--runtime", p},
		{"--status", "--runtime", p},
	} {
		if code := dispatch(args); code != exitNoDaemon {
			t.Errorf("%v 应路由到 status 命令，实际退出码 %d", args, code)
		}
	}
}
