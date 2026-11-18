package main

import (
	"testing"

	"cnal/internal/protocol"
)

// 行为：rebind 的动作序列必须是"查对端 → 拿到地址 → 带字段登录 → 复查"，
// 拿不到地址就绝不发登录（异校零影响）。
func TestRebindPlan(t *testing.T) {
	steps := PlanRebind(true, "2001:db8:1::19", nil)
	if len(steps) != 3 || steps[2] != "复查对端" {
		t.Errorf("有序列偏差: %v", steps)
	}
	none := PlanRebind(true, "", protocol.ErrPeerAbsent)
	if len(none) != 0 {
		t.Errorf("拿不到对端地址时不得发登录: %v", none)
	}
	off := PlanRebind(false, "2001:db8:1::19", nil)
	if len(off) != 1 || off[0] != "纯 v4 登录" {
		t.Errorf("开关 off 时只走 v4: %v", off)
	}
}
