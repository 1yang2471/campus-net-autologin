package netstack

import (
	"net"
	"testing"
)

// 行为：v6 源选择只接受 2000::/3 全局地址（隧道给的 fdfe::/fc00:: 段被滤掉）；
// 本机无 v6 时返回 nil（S0 静默语义，不是错误）。
func TestPhysicalSourceV6Shape(t *testing.T) {
	v6 := PhysicalSourceV6()
	if v6 == nil {
		t.Skip("本机无全局 v6 源，跳过（S0 语义）")
	}
	if v6.To4() != nil {
		t.Errorf("必须是 v6 地址，实际 %v", v6)
	}
	if !v6.IsGlobalUnicast() || v6[0]&0xE0 != 0x20 {
		t.Errorf("必须是 2000::/3 全局地址，实际 %v", v6)
	}
}

// 行为：公网 v6 过滤只留 2000::/3 全局地址——v4、环回、链路本地、ULA
// （隧道给的 fdfe::）都不是门户会话地址；重复项去重保序。
func TestPublicV6AddrsFiltersNonPublic(t *testing.T) {
	in := []net.IP{
		net.ParseIP("10.0.0.5"),
		net.ParseIP("2001:250:3003:aa29:56dd:a744:bd21:a019"),
		net.ParseIP("fe80::1"),
		net.ParseIP("fdfe:dcba:9876::1"),
		net.ParseIP("::1"),
		net.ParseIP("2001:250:3003:aa29:181c:d6e0:8ee0:4c3f"),
		net.ParseIP("2001:250:3003:aa29:56dd:a744:bd21:a019"),
	}
	got := PublicV6Addrs(in)
	if len(got) != 2 {
		t.Fatalf("应只剩稳定+临时两个公网 v6，实际 %v", got)
	}
	if got[0].String() != "2001:250:3003:aa29:56dd:a744:bd21:a019" ||
		got[1].String() != "2001:250:3003:aa29:181c:d6e0:8ee0:4c3f" {
		t.Errorf("过滤/去重/保序不对：%v", got)
	}
}

// 行为：同接口的其它公网 v6 是独立门户会话（Windows 隐私临时地址默认参与
// 出站）。切账号只搬稳定地址会出现「v4 换了、IPv6 仍是原账号」。
func TestV6SiblingsInExcludesPrimaryOnly(t *testing.T) {
	stable := net.ParseIP("2001:250:3003:aa29:56dd:a744:bd21:a019")
	temp := net.ParseIP("2001:250:3003:aa29:181c:d6e0:8ee0:4c3f")
	all := []net.IP{stable, temp, net.ParseIP("fdfe::1"), net.ParseIP("10.0.0.1")}
	got := V6SiblingsIn(stable, all)
	if len(got) != 1 || !got[0].Equal(temp) {
		t.Errorf("应只返回临时地址，实际 %v", got)
	}
	if got := V6SiblingsIn(stable, []net.IP{stable}); len(got) != 0 {
		t.Errorf("只有稳定地址时不该有兄弟项，实际 %v", got)
	}
}
