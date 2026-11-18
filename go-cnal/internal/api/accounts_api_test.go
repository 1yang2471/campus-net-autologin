package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 多账号切换契约：GET /accounts 历史列表（含 current 标记），
// POST /accounts {username} 切换（Python switch_account：密码从 keyring
// 按用户名还原，取不到必须拒绝——缝返回 error → 502）。

func newAccountTestServer(t *testing.T) (*Server, *httptest.Server, *string) {
	var calls int
	switched := new(string)
	s, ts := newAPITestServer(&calls, nil)
	s.Accounts = func() []Account {
		return []Account{
			{Username: "u2", Server: "https://b", Current: true},
			{Username: "u1", Server: "https://a"},
		}
	}
	s.SwitchAccount = func(user string) (string, error) {
		*switched = user
		return "已切换到 " + user, nil
	}
	return s, ts, switched
}

// 行为：GET /accounts 返回历史列表，按最近使用排序，current 标记正确。
func TestGetAccountsList(t *testing.T) {
	s, ts, _ := newAccountTestServer(t)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/accounts", nil)
	req.Header.Set("Authorization", "Bearer "+s.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("应 200，实际 %d", resp.StatusCode)
	}
	var accs []Account
	_ = json.NewDecoder(resp.Body).Decode(&accs)
	if len(accs) != 2 || accs[0].Username != "u2" || !accs[0].Current {
		t.Errorf("列表顺序/current 标记错误: %+v", accs)
	}
}

// 行为：POST /accounts {username} → 切换缝收到用户名。
func TestPostAccountsSwitch(t *testing.T) {
	s, ts, switched := newAccountTestServer(t)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/accounts",
		strings.NewReader(`{"username":"u1"}`))
	req.Header.Set("Authorization", "Bearer "+s.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("切换应 200，实际 %d", resp.StatusCode)
	}
	if *switched != "u1" {
		t.Errorf("切换缝应收到 u1，实际 %q", *switched)
	}
}

// 行为：切换缝报错（keyring 无该账号密码）→ 502，错误文本透传（不静默成功）。
func TestSwitchAccountErrorIs502(t *testing.T) {
	var calls int
	s, ts := newAPITestServer(&calls, nil)
	s.Accounts = func() []Account { return nil }
	s.SwitchAccount = func(user string) (string, error) {
		return "", errors.New("凭据管理器无该账号密码")
	}
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/accounts",
		strings.NewReader(`{"username":"ghost"}`))
	req.Header.Set("Authorization", "Bearer "+s.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("无凭据切换应 502，实际 %d", resp.StatusCode)
	}
}

// 行为：切换成功的**结果文案**必须回给面板——「已切换」和「认证被门户拒绝」
// 是两件事，面板不能只显示一句「已切换到 X」就完事（2026-11-09 事故）。
func TestPostAccountsReturnsSwitchMessage(t *testing.T) {
	var calls int
	s, ts := newAPITestServer(&calls, nil)
	s.Accounts = func() []Account { return nil }
	s.SwitchAccount = func(user string) (string, error) {
		return "已切换到 " + user + "（凭据已保存），但认证未成功：冻结剩余60s", nil
	}
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/accounts",
		strings.NewReader(`{"username":"u1"}`))
	req.Header.Set("Authorization", "Bearer "+s.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("应 200，实际 %d", resp.StatusCode)
	}
	var out struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("响应应是 JSON: %v", err)
	}
	if !strings.Contains(out.Message, "冻结剩余60s") {
		t.Errorf("结果文案必须透传门户原文，实际 %q", out.Message)
	}
}
