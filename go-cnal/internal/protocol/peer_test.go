package protocol

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// 行为：对端只接受 IP 字面量 + 默认端口 + http/https；域名形态必须被拒
// （避免 SSRF 与凭据改道，规格 §6.1 第 4 条）。
func TestPeerRejectsNonLiteralAddress(t *testing.T) {
	if err := ValidatePeer("http://portal.example.edu/"); err == nil {
		t.Errorf("域名对端必须拒绝（避免 SSRF 与凭据改道）")
	}
	if err := ValidatePeer("http://[2001:db8::2]/"); err != nil {
		t.Errorf("文档段 IPv6 字面量应通过: %v", err)
	}
	if err := ValidatePeer("http://[fe80::1]/"); err == nil {
		t.Errorf("链路本地必须拒绝")
	}
	if err := ValidatePeer("http://192.0.2.7:8080/"); err == nil {
		t.Errorf("非默认端口必须拒绝（两端点端口不对称是校事实，不许焊死）")
	}
	// 握手 URL 必须是裸端点：query/userinfo 都能把凭据偷渡进请求行
	if err := ValidatePeer("http://192.0.2.7/?userName=u"); err == nil {
		t.Errorf("带 query 的对端 URL 必须拒绝（凭据偷渡面）")
	}
	if err := ValidatePeer("http://u:p@192.0.2.7/"); err == nil {
		t.Errorf("带 userinfo 的对端 URL 必须拒绝（凭据偷渡面）")
	}
}

// Task 4 补（SSRF 分级）：私有/保留段不是自动拒绝项——门户就在对端网内；
// 但环回与链路本地必须无条件拒（SSRF 经典落点，含云元数据 169.254.169.254）。
func TestPeerPrivateRangesAllowedOnlyForUnauthenticatedHandshake(t *testing.T) {
	for _, ok := range []string{
		"http://10.129.17.1/", "http://192.168.1.1/", "http://[fc00::2]/",
		"http://[1::2]/", "http://172.29.1.1/",
	} {
		if err := ValidatePeer(ok); err != nil {
			t.Errorf("校园内私有/保留对端应放行，实际拒了 %s: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"http://127.0.0.1/", "http://[::1]/", "http://169.254.169.254/",
		"http://[fe80::1]/", "http://0.0.0.0/", "http://[ff02::1]/",
	} {
		if err := ValidatePeer(bad); err == nil {
			t.Errorf("SSRF 落点必须拒绝: %s", bad)
		}
	}
}

// 行为：无凭据是构造级的——握手表单由唯一函数构造，编码后逐字节不含凭据键
// （规格 §7 / 审查 P0-3；计划原测试用本机 stub 抓包，但环回必拒+非默认端口必拒
// 使 stub 永远过不了校验，构造级断言 + Task 7 真机输出才是可达的等价证明）。
// 操作名是词表（校事实）：由调用方传入，2026-11-07 实测真名 ipv4or6online。
func TestHandshakeFormCarriesNoCredentials(t *testing.T) {
	vals := url.Values{}
	for k, v := range HandshakeForm("ipv4or6online") {
		vals.Set(k, v)
	}
	body := vals.Encode()
	for _, forbidden := range []string{"username", "pwd", "password", "login"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
			t.Errorf("握手请求体含禁键 %q：%s", forbidden, body)
		}
	}
}

// 行为：提取值会随门户更新变；一次性校验后长期复用等于给未来的污染值开门，
// 每次 Query 前必须重新校验（Task 4 补追加用例 1）。
func TestPeerRevalidatedOnEveryUse(t *testing.T) {
	p := NewPeer("http://[2001:db8::2]/ac_portal/login.php", 5*time.Second)
	if !p.LastValidated().IsZero() {
		t.Errorf("新建 Peer 不应已校验过")
	}
	_, _ = p.Query(nil, "ipv4or6online") // 触发校验（src 为 nil 时发送会失败，但校验必须已发生）
	if p.ValidatedCount() < 1 {
		t.Errorf("每次请求前必须重新校验，实际次数 %d", p.ValidatedCount())
	}
}
