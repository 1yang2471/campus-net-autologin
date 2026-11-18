package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// v6 补绑契约：POST /rebindv6 → 触发一次带 ipv4or6 的登录动作。
// 与 /login（通用手动重连）分开的理由：面板可据 v6_state 决定按钮可用性，
// 且失败原因要带「补绑」上下文（不是普通重连）。

// 行为：带 token POST /rebindv6 → RebindV6 缝调用一次，返回 ok。
func TestRebindV6TriggersAction(t *testing.T) {
	var calls int
	s := New()
	s.Snapshot = func() Snapshot { return Snapshot{State: "online"} }
	s.Login = func() error { return nil }
	s.RebindV6 = func() error { calls++; return nil }
	ts := httptest.NewServer(s.Handler())
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/rebindv6", nil)
	req.Header.Set("Authorization", "Bearer "+s.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("补绑应 200，实际 %d", resp.StatusCode)
	}
	if calls != 1 {
		t.Errorf("补绑应触发动作一次，实际 %d", calls)
	}
}

// 行为：补绑失败 → 502 透传原因（门户原文，不静默成功）。
func TestRebindV6ErrorIs502(t *testing.T) {
	s := New()
	s.Snapshot = func() Snapshot { return Snapshot{State: "online"} }
	s.Login = func() error { return nil }
	s.RebindV6 = func() error { return errors.New("门户未受理补绑") }
	ts := httptest.NewServer(s.Handler())
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/rebindv6", nil)
	req.Header.Set("Authorization", "Bearer "+s.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("失败应 502，实际 %d", resp.StatusCode)
	}
}

// 行为：缝未接线 → 501（cnald 无头壳不接补绑缝时诚实回答）。
func TestRebindV6SeamUnwiredIs501(t *testing.T) {
	var calls int
	s, ts := newAPITestServer(&calls, nil)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/rebindv6", nil)
	req.Header.Set("Authorization", "Bearer "+s.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("缝未接线应 501，实际 %d", resp.StatusCode)
	}
}

// 行为：GET /rebindv6 → 405（只允许 POST，防误触）。
func TestRebindV6GetRejected(t *testing.T) {
	var calls int
	s, ts := newAPITestServer(&calls, nil)
	st, _ := getJSON(t, ts.URL+"/rebindv6", s.Token)
	if st != http.StatusMethodNotAllowed && st != http.StatusNotImplemented {
		t.Errorf("GET 应 405/501，实际 %d", st)
	}
}
