package protocol

import "testing"

// 性能基线（复盘 §3.5）：协议层热路径。载荷取真机/语料真实应答形态。
// 跑法：go test ./internal/protocol/ -bench . -benchmem -run '^$'

var (
	benchHandshake   = `{'success':true, 'ip':'2001:db8:1::19', 'isOnline':true}`
	benchOnlineCheck = `{"success":true, "online":1, "username":"USER"}`
)

func BenchmarkInterpretHandshake(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if got := InterpretHandshake(benchHandshake); got != StatePeerOnline {
			b.Fatalf("真机载荷应读成 online，实际 %s", got)
		}
	}
}

func BenchmarkParseOnlineCheck(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		st, err := ParseOnlineCheck(benchOnlineCheck)
		if err != nil || !st.Online {
			b.Fatalf("真机载荷应读成在线: %v %+v", err, st)
		}
	}
}

func BenchmarkFormFields(b *testing.B) {
	b.ReportAllocs()
	f := DefaultForm()
	for i := 0; i < b.N; i++ {
		_ = f.Fields("user", "pass")
	}
}
