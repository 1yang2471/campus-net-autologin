package dialect

import "testing"

// 性能基线（复盘 §3.5）：dialect.Parse 是每个握手/online_check 应答都过的
// 热路径。载荷取真机应答形态（单引号 Python 字典风格，门户真实返回）。
// 跑法：go test ./internal/dialect/ -bench . -benchmem -run '^$'

var benchPayload = `{'success':true, 'ip':'2001:db8:1::19', 'isOnline':false}`

func BenchmarkParse(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Parse(benchPayload); err != nil {
			b.Fatalf("解析失败: %v", err)
		}
	}
}

// TestParseAllocBudget 分配回归闸（复盘 §3.5「回归闸」）：跑在普通 go test
// 里。判据先量再写——2026-11-12 实测 12 allocs/op，预算 16 留 ~33% 余量；
// 超了说明热路径引进了额外分配（正则、多余拷贝、map 重建），先看再说。
func TestParseAllocBudget(t *testing.T) {
	const allocBudget = 16
	got := testing.AllocsPerRun(1000, func() { _, _ = Parse(benchPayload) })
	if got > allocBudget {
		t.Fatalf("dialect.Parse 分配回归: %.0f > %d allocs/op（2026-11-12 基线 12）",
			got, allocBudget)
	}
}
