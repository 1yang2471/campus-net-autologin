package protocol

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"

	"cnal/internal/dialect"
)

type step struct {
	When     string            `json:"when"`
	Request  map[string]string `json:"request"`
	Response string            `json:"response"`
	Expect   map[string]any    `json:"expect"`
}

type scenario struct {
	Name  string `json:"name"`
	Note  string `json:"note"`
	Steps []step `json:"steps"`
}

func loadScenario(t *testing.T, path string) scenario {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("语料缺失：跑计划前置 Step 2.1（%v）", err)
	}
	var s scenario
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("语料解析失败: %v", err)
	}
	return s
}

// 行为：握手响应的 isOnline=false 必须被读成"有地址但未在线"（peerState=offline），
// 而不是"不可观测"，也不是"没有 v6"。这是 S2b 态，唯一允许触发补绑的态。
func TestHandshakeReadsOffline(t *testing.T) {
	sc := loadScenario(t, "../../testdata/scenarios/dual-stack-rebind.json")
	got := InterpretHandshake(sc.Steps[0].Response)
	if got != StatePeerOffline {
		t.Errorf("应判为 S2b（对端未在线），实际 %v", got)
	}
}

// 跨实现裁判（Task 8）：与 Python scenario_runner.py 跑同一份语料——
// 任一边方言改动，另一边必须变红。握手步判四态；登录步断"现场取值的
// 表单必带握手步观察到的对端 ip"并与语料请求自洽。
func TestScenarioCorpus(t *testing.T) {
	sc := loadScenario(t, "../../testdata/scenarios/dual-stack-rebind.json")
	var peerIP string
	for i, st := range sc.Steps {
		switch st.Request["opr"] {
		case "ipv4or6online":
			got := InterpretHandshake(st.Response)
			m, _ := dialect.Parse(st.Response)
			if want, ok := st.Expect["peerState"].(string); ok {
				if string(got) != want {
					t.Errorf("step%d: peerState 期望 %v 实际 %v", i, want, got)
				}
				if ip, _ := m["ip"].(string); ip != "" {
					peerIP = ip
				}
			}
			if want, ok := st.Expect["selfState"].(string); ok {
				if string(got) != want {
					t.Errorf("step%d: selfState 期望 %v 实际 %v", i, want, got)
				}
			}
		case "pwdLogin":
			// 语料自洽：登录步请求的 ipv4or6 必须等于握手步观察到的对端 ip
			if reqIP := st.Request["ipv4or6"]; reqIP != "" && reqIP != peerIP {
				t.Errorf("step%d: 语料自洽——ipv4or6=%s ≠ 握手步 ip=%s", i, reqIP, peerIP)
			}
			p := NewPeerProvider(func() (string, error) { return peerIP, nil })
			form := EncodeForm(BuildLogin("u", "p", Settings{DualStack: true}, p))
			if !strings.Contains(form, "ipv4or6="+url.QueryEscape(peerIP)) {
				t.Errorf("step%d: 现场取值表单应带 ipv4or6=%s，实际 %s", i, peerIP, form)
			}
			m, _ := dialect.Parse(st.Response)
			if ok, _ := m["success"].(bool); !ok {
				t.Errorf("step%d: 语料登录应答应 success=true", i)
			}
		default:
			t.Errorf("step%d: 裁判不认识的 opr %q——语料扩了新操作必须同步裁判", i, st.Request["opr"])
		}
	}
}
