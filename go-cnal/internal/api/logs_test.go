package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 可视化日志契约（2026-11-08 用户实证：托盘「查看日志」直接开文件夹是占位）。
// GET /logs → 日志尾部行数组（新的在最后）；缝未接线 501；读失败 502。

func newLogsTestServer(t *testing.T, lines []string, err error) (*Server, *httptest.Server) {
	var calls int
	s, ts := newAPITestServer(&calls, nil)
	s.Logs = func() ([]string, error) { return lines, err }
	return s, ts
}

// 行为：GET /logs 返回行数组原样透传（面板按行渲染）。
func TestLogsPassthrough(t *testing.T) {
	s, ts := newLogsTestServer(t, []string{"a 在线", "b 离线"}, nil)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/logs", nil)
	req.Header.Set("Authorization", "Bearer "+s.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("应 200，实际 %d", resp.StatusCode)
	}
	var got []string
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1] != "b 离线" {
		t.Errorf("行数组应透传，实际 %v", got)
	}
}

// 行为：读日志失败 → 502 透传原因（不假装空日志）。
func TestLogsErrorIs502(t *testing.T) {
	s, ts := newLogsTestServer(t, nil, errors.New("日志读取失败"))
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/logs", nil)
	req.Header.Set("Authorization", "Bearer "+s.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("读失败应 502，实际 %d", resp.StatusCode)
	}
}

// 行为：缝未接线 → 501。
func TestLogsSeamUnwiredIs501(t *testing.T) {
	var calls int
	s, ts := newAPITestServer(&calls, nil)
	st, _ := getJSON(t, ts.URL+"/logs", s.Token)
	if st != http.StatusNotImplemented {
		t.Errorf("缝未接线应 501，实际 %d", st)
	}
}
