package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 面板设置契约（补齐用户指出的缺口：账号/密码/服务器无处可改）：
// GET /config → 当前非敏感配置（username/server/free_windows 等，密码绝不回传）；
// POST /config → 保存。凭据经 SaveCredentials 缝落 Windows 凭据管理器，
// ini 由 SaveConfigFile 缝改写——api 包不直接碰文件/注册表（可测性）。

func newCfgTestServer(t *testing.T) (*Server, *httptest.Server, *[]map[string]string) {
	var calls int
	s, ts := newAPITestServer(&calls, nil)
	saved := &[]map[string]string{}
	s.GetConfig = func() map[string]string {
		return map[string]string{
			"username": "23jszeng", "server": "https://a.stu.edu.cn",
			"free_windows": "23:30-06:30", "campus_dns": "10.129.17.10",
		}
	}
	s.SaveCredentials = func(user, pass string) error {
		*saved = append(*saved, map[string]string{"user": user, "pass": pass})
		return nil
	}
	s.SaveConfig = func(values map[string]string) error {
		*saved = append(*saved, values)
		return nil
	}
	return s, ts, saved
}

// 行为：GET /config 返回当前配置；密码字段绝不出现在响应里。
func TestGetConfigHidesPassword(t *testing.T) {
	s, ts, _ := newCfgTestServer(t)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/config", nil)
	req.Header.Set("Authorization", "Bearer "+s.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("应 200，实际 %d", resp.StatusCode)
	}
	var m map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&m)
	if m["username"] != "23jszeng" || m["server"] != "https://a.stu.edu.cn" {
		t.Errorf("应返回 username/server，实际 %v", m)
	}
	for k := range m {
		if strings.Contains(strings.ToLower(k), "pass") {
			t.Errorf("密码字段不得出现在配置响应: %q", k)
		}
	}
}

// 行为：POST /config 带新账号+密码 → SaveCredentials 落凭据、SaveConfig 落 ini，
// 都被调用且参数正确；只改 server 不带密码 → 不动凭据。
func TestPostConfigSavesCredentialsAndIni(t *testing.T) {
	s, ts, saved := newCfgTestServer(t)
	body := `{"username":"newuser","password":"newpass","server":"https://a.stu.edu.cn"}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/config", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("应 200，实际 %d", resp.StatusCode)
	}
	if len(*saved) != 2 {
		t.Fatalf("应落凭据+改 ini 各一次，实际 %d 次写", len(*saved))
	}
	if (*saved)[0]["user"] != "newuser" || (*saved)[0]["pass"] != "newpass" {
		t.Errorf("凭据参数应为新账号，实际 %v", (*saved)[0])
	}
	if (*saved)[1]["server"] != "https://a.stu.edu.cn" {
		t.Errorf("ini 写入应含 server，实际 %v", (*saved)[1])
	}
}

// 行为：POST 不带 password → 只改 ini，凭据不动（改服务器不必重输密码）。
func TestPostConfigWithoutPasswordKeepsCredentials(t *testing.T) {
	s, ts, saved := newCfgTestServer(t)
	body := `{"server":"https://b.stu.edu.cn"}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/config", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+s.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("应 200，实际 %d", resp.StatusCode)
	}
	if len(*saved) != 1 {
		t.Fatalf("只应写 ini 一次，实际 %d 次写", len(*saved))
	}
}

// 行为：空 username 但带 password → 拒绝（密码必须与账号成对保存）。
func TestPostConfigPasswordWithoutUserRejected(t *testing.T) {
	s, ts, saved := newCfgTestServer(t)
	body := `{"password":"orphan"}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/config", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+s.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("缺账号的密码保存应 400，实际 %d", resp.StatusCode)
	}
	if len(*saved) != 0 {
		t.Errorf("拒绝时不得有任何写入，实际 %d 次", len(*saved))
	}
}

// 行为：空密码 → 400 且不得写凭据（空 blob 会 panic，且会覆盖真密码；
// 2026-11-08 发布审查 P0）。
func TestPostConfigEmptyPasswordRejected(t *testing.T) {
	s, ts, saved := newCfgTestServer(t)
	body := `{"username":"u","password":""}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/config", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("空密码应 400，实际 %d", resp.StatusCode)
	}
	if len(*saved) != 0 {
		t.Errorf("空密码不得写任何凭据/ini，实际 %d 次", len(*saved))
	}
}

// 行为：只给空 username（不带 password）→ 400；不得把 ini 里的账号清空
// （旧条件放过这个组合，SaveConfig 会写 username = 空串）。
func TestPostConfigEmptyUsernameAloneRejected(t *testing.T) {
	s, ts, saved := newCfgTestServer(t)
	body := `{"username":""}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/config", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("空 username 单独出现应 400，实际 %d", resp.StatusCode)
	}
	if len(*saved) != 0 {
		t.Errorf("拒绝时不得有任何写入，实际 %d 次", len(*saved))
	}
}

// 行为：业务校验先于写凭据/ini——非法免认证时段带账号密码一起提交时，
// 必须 400 且一次写入都不发生（不得半写）。
func TestPostConfigInvalidFreeWindowsRejectedBeforeWrite(t *testing.T) {
	s, ts, saved := newCfgTestServer(t)
	s.ValidateConfig = func(values map[string]string) error {
		if values["free_windows"] == "25:00-06:30" {
			return errors.New("第 1 段 \"25:00-06:30\" 非法")
		}
		return nil
	}
	body := `{"free_windows":"25:00-06:30","username":"u","password":"p"}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/config", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("非法免认证时段应 400，实际 %d", resp.StatusCode)
	}
	if len(*saved) != 0 {
		t.Errorf("校验失败时不得写凭据或 ini，实际 %d 次", len(*saved))
	}
}
