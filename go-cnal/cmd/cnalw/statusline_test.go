package main

import (
	"strings"
	"testing"

	"cnal/internal/api"
)

func TestStatusLineShowsUserPathOnlyWhenBroken(t *testing.T) {
	s := api.Snapshot{Label: "在线", Detail: "出口受限（观察）"}
	if got := statusLine(s); got != "在线｜出口受限（观察）" {
		t.Errorf("基础拼接不对：%q", got)
	}
	s.UserPath = "broken"
	if !strings.Contains(statusLine(s), "用户路径不通") {
		t.Errorf("用户路径断时必须说：%q", statusLine(s))
	}
	s.UserPath = "ok"
	if strings.Contains(statusLine(s), "用户路径") {
		t.Errorf("用户路径正常时不占长度：%q", statusLine(s))
	}
	s.Detail = ""
	if got := statusLine(s); got != "在线" {
		t.Errorf("空详情不得留分隔符：%q", got)
	}
}
