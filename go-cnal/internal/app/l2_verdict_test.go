package app

import "testing"

// L2 双路对照判定表（2026-11-22 复盘内因 1）：campus 死+系统活=dns_broken；
// 双死=restricted；campus 活=ok；campus 目标不可得走回退路语义。
// 场景对应 11-22 事故：campus_dns 给假 IP（解析活、TLS 死）而系统栈实测可用
// ——修复前 4.5h 假警报「出口受限」，与真·AC 拉黑混为一谈。
func TestL2VerdictTable(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		campusTargeted, campusOK bool
		sysTargeted, sysOK       bool
		want                     string
	}{
		{"campus 活→ok（对照路不触网）", true, true, false, false, "ok"},
		{"campus 死+系统活→dns_broken", true, false, true, true, "dns_broken"},
		{"campus 死+系统死→restricted", true, false, true, false, "restricted"},
		{"campus 死+系统不可测→restricted（保守）", true, false, false, false, "restricted"},
		{"campus 不可得+系统死→restricted", false, false, true, false, "restricted"},
		{"campus 不可得+系统活→ok", false, false, true, true, "ok"},
		{"campus 不可得+系统不可得→ok（放弃观察）", false, false, false, false, "ok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := l2Verdict(tc.campusTargeted, tc.campusOK, tc.sysTargeted, tc.sysOK)
			if got != tc.want {
				t.Errorf("l2Verdict(%v,%v,%v,%v)=%q，应 %q",
					tc.campusTargeted, tc.campusOK, tc.sysTargeted, tc.sysOK, got, tc.want)
			}
		})
	}
}
