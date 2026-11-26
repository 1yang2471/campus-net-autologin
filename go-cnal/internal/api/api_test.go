package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// S1 回路契约（设计规格 §4）：仅 127.0.0.1、启动时随机 token；GET /state 快照、
// POST /login 手动重连（手动意图）。词表与 Python kernel_api 逐字对齐
// （跨实现互裁：同语料同语义），面板/托盘都吃这份契约，不各自编码。

func newAPITestServer(loginCalls *int, loginErr error) (*Server, *httptest.Server) {
	s := New()
	s.Snapshot = func() Snapshot {
		return Snapshot{
			State:  "online",
			Detail: "",
			Extra:  map[string]bool{"session_dead": false, "free_mode": false},
		}
	}
	s.Login = func() error {
		*loginCalls++
		return loginErr
	}
	ts := httptest.NewServer(s.Handler())
	return s, ts
}

func getJSON(t *testing.T, url, token string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return resp.StatusCode, m
}

// 行为：无 token / 错 token 一律 401——回路 API 的唯一防线就是它。
func TestTokenRequired(t *testing.T) {
	var calls int
	s, ts := newAPITestServer(&calls, nil)
	if st, _ := getJSON(t, ts.URL+"/state", ""); st != http.StatusUnauthorized {
		t.Errorf("无 token 应 401，实际 %d", st)
	}
	if st, _ := getJSON(t, ts.URL+"/state", "wrong"); st != http.StatusUnauthorized {
		t.Errorf("错 token 应 401，实际 %d", st)
	}
	if s.Token == "" {
		t.Error("token 必须构造时生成且非空")
	}
	if len(s.Token) < 16 {
		t.Errorf("token 至少 16 字符随机熵，实际 %d", len(s.Token))
	}
}

// 行为：带 token GET /state → 状态快照，label/color 与 Python 词表逐字一致。
func TestStateSnapshotParity(t *testing.T) {
	var calls int
	s, ts := newAPITestServer(&calls, nil)
	st, m := getJSON(t, ts.URL+"/state", s.Token)
	if st != http.StatusOK {
		t.Fatalf("带 token 应 200，实际 %d", st)
	}
	if m["state"] != "online" || m["label"] != "在线" || m["color"] != "#4f9d63" {
		t.Errorf("快照词表必须与 Python kernel_api 逐字对齐，实际 %v", m)
	}
}

// 行为：带 token POST /login → 手动重连一次；返回 ok 或失败原因（不暴露堆栈）。
func TestLoginActionManualIntent(t *testing.T) {
	calls := 0
	s, ts := newAPITestServer(&calls, nil)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/login", nil)
	req.Header.Set("Authorization", "Bearer "+s.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("手动重连应 200，实际 %d", resp.StatusCode)
	}
	if calls != 1 {
		t.Errorf("POST /login 应触发登录缝一次，实际 %d", calls)
	}
}

// 行为：未知路径 404，不带任何状态信息。
func TestUnknownPathIs404(t *testing.T) {
	var calls int
	_, ts := newAPITestServer(&calls, nil)
	st, _ := getJSON(t, ts.URL+"/etc", New().Token)
	if st != http.StatusNotFound {
		t.Errorf("未知路径应 404，实际 %d", st)
	}
}

// 行为：全状态词表齐（含 paused/starting），少一个都是契约漂移。
func TestStateVocabularyComplete(t *testing.T) {
	for _, st := range []string{"online", "offline", "portal_down", "rejected", "free_window", "paused", "starting"} {
		if _, ok := stateLabels[st]; !ok {
			t.Errorf("状态 %q 缺 label", st)
		}
		if _, ok := stateColors[st]; !ok {
			t.Errorf("状态 %q 缺 color", st)
		}
	}
	if !strings.HasPrefix(stateColors["online"], "#") {
		t.Error("color 必须是 #rrggbb 形态")
	}
}

// 行为：Label 导出词表访问（未知状态回退状态名本身，Python kernel_api 同语义）。
func TestLabelAccessor(t *testing.T) {
	if Label("online") != "在线" {
		t.Errorf("online 应为 在线，实际 %q", Label("online"))
	}
	if Label("no_such_state") != "no_such_state" {
		t.Errorf("未知状态应回退状态名，实际 %q", Label("no_such_state"))
	}
}

// 行为：Color 导出词表访问——装配层（托盘图标）靠它取色，闭包不自己再抄一份。
// 2026-11-07 实证：Snapshot 闭包漏填 Color → 托盘图标解析失败被静默跳过，
// 托盘项存在但图标空白。
func TestColorAccessor(t *testing.T) {
	if Color("online") != "#4f9d63" {
		t.Errorf("online 色应为 #4f9d63，实际 %q", Color("online"))
	}
	if Color("no_such_state") != "#8b919b" {
		t.Errorf("未知状态应回退中性灰，实际 %q", Color("no_such_state"))
	}
}

// 行为：快照的物理源读数原样透传到 JSON（面板 IPv4/IPv6 行取这两个字段，
// 不与状态大卡的 label 重复——deletion test：删了就少一份「从哪张网卡出」信息）。
func TestSnapshotSourceFieldsPassThrough(t *testing.T) {
	s := New()
	s.Login = func() error { return nil }
	s.Snapshot = func() Snapshot {
		return Snapshot{State: "online", V4Src: "172.29.153.208", V6Src: "2001:db8::1"}
	}
	ts := httptest.NewServer(s.Handler())
	_, m := getJSON(t, ts.URL+"/state", s.Token)
	if m["v4_src"] != "172.29.153.208" || m["v6_src"] != "2001:db8::1" {
		t.Errorf("物理源字段应透传，实际 %v", m)
	}
}

// 行为：带 token GET /flux → 流量 dict 原样透传（Python kernel_api.fetch_flux
// 同语义：{'用户名称':…, '已用流量':…}）。查询缝与 Snapshot 同源——面板/托盘
// 查流量都吃这份契约，不各自触网。
func TestFluxPassthrough(t *testing.T) {
	var calls int
	s := New()
	s.Snapshot = func() Snapshot { return Snapshot{State: "online"} }
	s.Login = func() error { return nil }
	s.FetchFlux = func() (map[string]string, error) {
		calls++
		return map[string]string{"用户名称": "23ykyang", "已用流量": "1.2 GB"}, nil
	}
	ts := httptest.NewServer(s.Handler())
	st, m := getJSON(t, ts.URL+"/flux", s.Token)
	if st != http.StatusOK {
		t.Fatalf("带 token 应 200，实际 %d", st)
	}
	if m["已用流量"] != "1.2 GB" {
		t.Errorf("流量表应原样透传，实际 %v", m)
	}
	if calls != 1 {
		t.Errorf("GET /flux 应触发查询缝一次，实际 %d", calls)
	}
}

// 行为：查询失败 → 502 + 错误文本（不造假数据，面板据此显示失败原因）。
func TestFluxErrorIs502(t *testing.T) {
	s := New()
	s.Snapshot = func() Snapshot { return Snapshot{State: "online"} }
	s.Login = func() error { return nil }
	s.FetchFlux = func() (map[string]string, error) {
		return nil, errors.New("门户解析失败")
	}
	ts := httptest.NewServer(s.Handler())
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/flux", nil)
	req.Header.Set("Authorization", "Bearer "+s.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("查询失败应 502，实际 %d", resp.StatusCode)
	}
}

// 行为：缝未接线 → 501（cnald 无头壳不接流量缝时的诚实回答，对齐 /config）。
func TestFluxSeamUnwiredIs501(t *testing.T) {
	var calls int
	s, ts := newAPITestServer(&calls, nil)
	if st, _ := getJSON(t, ts.URL+"/flux", s.Token); st != http.StatusNotImplemented {
		t.Errorf("缝未接线应 501，实际 %d", st)
	}
}

// 行为：?token= 只放行 /panel；状态变更端点必须 Bearer 头
// （防 URL token 经 Referer/历史泄漏后被拿去改配置/触发登录）。
func TestQueryTokenOnlyForPanel(t *testing.T) {
	var calls int
	s, ts := newAPITestServer(&calls, nil)
	tok := s.Token
	// /state 用 ?token= 必须 401
	if st, _ := getJSON(t, ts.URL+"/state?token="+tok, ""); st != http.StatusUnauthorized {
		t.Errorf("/state 用 query token 应 401，实际 %d", st)
	}
	// /panel 用 ?token= 放行（浏览器顶层导航）
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/panel?token="+tok, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound { // 未设 PanelPage → 404，说明过了鉴权
		t.Errorf("/panel 应放行 query token（404=已过 auth），实际 %d", resp.StatusCode)
	}
}

// 行为：接口绑定诊断字段透传且空错误省略，旧客户端零影响。
func TestSnapshotBindingFieldsPassThrough(t *testing.T) {
	s := New()
	s.Login = func() error { return nil }
	s.Snapshot = func() Snapshot {
		return Snapshot{
			State:         "online",
			BindMode:      "interface",
			BindInterface: "WLAN 2",
			BindIndex:     9,
			BindDegraded:  false,
			BindError:     "",
		}
	}
	ts := httptest.NewServer(s.Handler())
	_, m := getJSON(t, ts.URL+"/state", s.Token)
	if m["bind_mode"] != "interface" || m["bind_interface"] != "WLAN 2" || m["bind_index"] != float64(9) {
		t.Fatalf("绑定诊断字段应透传，实际 %v", m)
	}
	if _, ok := m["bind_error"]; ok {
		t.Fatal("空绑定错误应被 omitempty 省略")
	}
}

// 行为：门户路径读数原样透传到 JSON（面板「门户路径」行；2026-11-11 设计 §4）。
func TestSnapshotRoutePassThrough(t *testing.T) {
	s := New()
	s.Login = func() error { return nil }
	s.Snapshot = func() Snapshot {
		return Snapshot{State: "online", Route: "绑源钉 IP（钉 1.1.1.2:443）"}
	}
	ts := httptest.NewServer(s.Handler())
	_, m := getJSON(t, ts.URL+"/state", s.Token)
	if m["route"] != "绑源钉 IP（钉 1.1.1.2:443）" {
		t.Errorf("门户路径字段应透传，实际 %v", m)
	}
}
