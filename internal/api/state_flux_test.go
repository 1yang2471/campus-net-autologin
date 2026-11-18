package api

import (
	"net/http/httptest"
	"testing"

	"cnal/internal/keeper"
)

// 切片5：/state 快照带上流量视图，面板刷 /state 时顺带拿到，不再单独打 /flux。
// 通过公开 HTTP 端点验证（不碰内部字段），omitempty 保证旧客户端零影响。

func TestStateIncludesFluxUsage(t *testing.T) {
	s := New()
	s.Snapshot = func() Snapshot {
		return Snapshot{
			State: "online",
			Flux:  &keeper.FluxUsageView{Used: "2.40006G", Quota: "4G", Percent: 60, HasPercent: true, Level: "ok"},
		}
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	status, m := getJSON(t, ts.URL+"/state", s.Token)
	if status != 200 {
		t.Fatalf("GET /state = %d", status)
	}
	flux, ok := m["flux"].(map[string]any)
	if !ok {
		t.Fatalf("/state 缺 flux 字段：%v", m)
	}
	if flux["used"] != "2.40006G" || flux["quota"] != "4G" {
		t.Errorf("flux 原文不对：%v", flux)
	}
	if flux["has_percent"] != true {
		t.Errorf("flux.has_percent 应为 true：%v", flux)
	}
	if p, _ := flux["percent"].(float64); p < 59 || p > 61 {
		t.Errorf("flux.percent 应约 60，实际 %v", flux["percent"])
	}
}

// 行为：未接线时 flux 省略（omitempty），旧客户端读到的 JSON 形状不变。
func TestStateOmitsFluxWhenUnwired(t *testing.T) {
	s := New()
	s.Snapshot = func() Snapshot { return Snapshot{State: "online"} }
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	_, m := getJSON(t, ts.URL+"/state", s.Token)
	if _, ok := m["flux"]; ok {
		t.Errorf("未接线时 flux 应省略，实际 %v", m)
	}
}
