package app

import (
	"testing"
	"time"
)

// [response] 词表与 reject_backoff/transient_backoff 配置面（异校适配的
// 两个 P0 键）：默认值与 Python DEFAULTS/RESPONSE_DEFAULTS 同源，覆盖后
// 如实进入 daemonConfig，交给 keeper 消费。

func TestConfigResponseMarksAndBackoff(t *testing.T) {
	c, err := loadDaemonConfig(writeTmpIni(t, `[keeper]
server = https://x
login_path = /a
reject_backoff = 120
transient_backoff = 45

[response]
success_marks = OK, 搞定
negative_marks = denied
transient_marks = busy, retry
freeze_marks = 锁定
hijack_marks = ac_portal, myPortal
`))
	if err != nil {
		t.Fatal(err)
	}
	if c.RejectBackoff != 120*time.Second {
		t.Errorf("reject_backoff 应 120s，实际 %v", c.RejectBackoff)
	}
	if c.TransientBackoff != 45*time.Second {
		t.Errorf("transient_backoff 应 45s，实际 %v", c.TransientBackoff)
	}
	if got := c.Response.Success; len(got) != 2 || got[0] != "OK" || got[1] != "搞定" {
		t.Errorf("success_marks 应 [OK 搞定]，实际 %v", got)
	}
	if got := c.Response.Negative; len(got) != 1 || got[0] != "denied" {
		t.Errorf("negative_marks 应 [denied]，实际 %v", got)
	}
	if got := c.Response.Transient; len(got) != 2 || got[0] != "busy" || got[1] != "retry" {
		t.Errorf("transient_marks 应 [busy retry]，实际 %v", got)
	}
	if got := c.Response.Freeze; len(got) != 1 || got[0] != "锁定" {
		t.Errorf("freeze_marks 应 [锁定]，实际 %v", got)
	}
	// Python RESPONSE_DEFAULTS 的 hijack_marks 也在 [response]：迁移不能丢。
	if got := c.HijackMarks; len(got) != 2 || got[0] != "ac_portal" || got[1] != "myPortal" {
		t.Errorf("hijack_marks 应 [ac_portal myPortal]，实际 %v", got)
	}
}

// 行为：缺省 → 退避 600/30 与默认词表（参考校零改动）。
func TestConfigResponseAndBackoffDefaults(t *testing.T) {
	c, err := loadDaemonConfig(writeTmpIni(t, "[keeper]\nserver = https://x\nlogin_path = /a\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.RejectBackoff != 600*time.Second || c.TransientBackoff != 30*time.Second {
		t.Errorf("退避缺省应 600/30，实际 %v/%v", c.RejectBackoff, c.TransientBackoff)
	}
	if len(c.Response.Success) == 0 || c.Response.Success[0] != "成功" {
		t.Errorf("success_marks 缺省应含「成功」，实际 %v", c.Response.Success)
	}
	if len(c.Response.Transient) == 0 || c.Response.Transient[0] != "noauth" {
		t.Errorf("transient_marks 缺省应含 noauth，实际 %v", c.Response.Transient)
	}
	if len(c.Response.Freeze) == 0 || c.Response.Freeze[0] != "冻结" {
		t.Errorf("freeze_marks 缺省应含「冻结」，实际 %v", c.Response.Freeze)
	}
}

// 行为：数值越界按 Python _num 范围钳制（reject 60..86400、transient 5..600）。
func TestConfigBackoffClampedToPythonRange(t *testing.T) {
	c, err := loadDaemonConfig(writeTmpIni(t, "[keeper]\nserver = https://x\nlogin_path = /a\nreject_backoff = 5\ntransient_backoff = 9999\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.RejectBackoff != 60*time.Second {
		t.Errorf("reject_backoff 下限应钳到 60s，实际 %v", c.RejectBackoff)
	}
	if c.TransientBackoff != 600*time.Second {
		t.Errorf("transient_backoff 上限应钳到 600s，实际 %v", c.TransientBackoff)
	}
}

// 行为：非整数仍是可见错误（不静默回落，用户会以为配置生效了）。
func TestConfigBackoffRejectsNonInteger(t *testing.T) {
	if _, err := loadDaemonConfig(writeTmpIni(t, "[keeper]\nserver = https://x\nlogin_path = /a\nreject_backoff = 10m\n")); err == nil {
		t.Fatal("非整数 reject_backoff 应报错")
	}
}
