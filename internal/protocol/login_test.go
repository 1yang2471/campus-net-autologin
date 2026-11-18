package protocol

import (
	"errors"
	"strings"
	"testing"
)

func TestLoginFormCarriesPeerOnlyWhenKnown(t *testing.T) {
	known := NewPeerProvider(func() (string, error) { return "2001:db8:1::19", nil })
	f := BuildLogin("u", "p", Settings{DualStack: true}, known)
	body := EncodeForm(f)
	if !strings.Contains(body, "ipv4or6=2001%3Adb8%3A1%3A%3A19") {
		t.Errorf("dual_stack 开且取到值时应带字段：%s", body)
	}

	absent := NewPeerProvider(func() (string, error) { return "", ErrPeerAbsent })
	f2 := BuildLogin("u", "p", Settings{DualStack: true}, absent)
	if strings.Contains(EncodeForm(f2), "ipv4or6") {
		t.Errorf("取不到值（absent）时请求体必须完全不含该字段：%s", EncodeForm(f2))
	}

	off := BuildLogin("u", "p", Settings{DualStack: false}, known)
	if strings.Contains(EncodeForm(off), "ipv4or6") {
		t.Errorf("开关 off 时必须退回纯 v4 请求体：%s", EncodeForm(off))
	}
}

// 行为：失败(fail) 与 缺失(absent) 不等价——fail 要能观测到（日志/状态），
// absent 是正常降级。混为一谈会把"门户没入口"报成"网络坏了"。
func TestPeerProviderDistinguishesAbsentFromFail(t *testing.T) {
	fail := NewPeerProvider(func() (string, error) { return "", ErrPeerQuery })
	if _, err := fail.Value(); err == ErrPeerAbsent {
		t.Errorf("查询失败不得塌缩成缺失")
	}
	if _, err := fail.Value(); !errors.Is(err, ErrPeerQuery) {
		t.Errorf("fail 应保留原错误，实际 %v", err)
	}
}

// 行为：v6 临时地址会轮换，值必须每次登录现场取，不能缓存。
// （计划原文经 EncodeForm 触发计数、而 BuildLogin 单次取值，语义自相矛盾；
// 改为两次 BuildLogin 计数——计划的变异检查"把结果缓存进字段"仍能翻红。）
func TestProviderIsCalledFreshEveryLogin(t *testing.T) {
	calls := 0
	p := NewPeerProvider(func() (string, error) { calls++; return "2001:db8:1::1", nil })
	_ = BuildLogin("u", "p", Settings{DualStack: true}, p)
	_ = BuildLogin("u", "p", Settings{DualStack: true}, p)
	if calls != 2 {
		t.Errorf("每次登录必须现场取值，不能缓存（调用 %d 次）", calls)
	}
}
