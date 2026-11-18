package keeper

import "testing"

// 免认证窗口解析（移植 Python t38 语义）：跨午夜窗口、多段、非法项跳过不致命。
func TestParseFreeWindows(t *testing.T) {
	ws := ParseFreeWindows("23:30-06:30,12:00-12:30")
	if len(ws) != 2 {
		t.Fatalf("应解析出 2 段，实际 %d", len(ws))
	}
	if !InFreeWindow(ws, 23*60+45) {
		t.Error("23:45 应命中跨午夜前段")
	}
	if !InFreeWindow(ws, 6*60) {
		t.Error("06:00 应命中跨午夜后段")
	}
	if !InFreeWindow(ws, 12*60+10) {
		t.Error("12:10 应命中午休窗口")
	}
	if InFreeWindow(ws, 12*60+40) {
		t.Error("12:40 不应命中任何窗口")
	}
}

func TestParseFreeWindowsEmptyIsAlwaysFalse(t *testing.T) {
	if InFreeWindow(ParseFreeWindows(""), 23*60+45) {
		t.Error("未配置时恒 false（行为不变）")
	}
}

func TestParseFreeWindowsSkipsInvalid(t *testing.T) {
	ws := ParseFreeWindows("abc,25:00-99:99,01:00-02:00")
	if len(ws) != 1 {
		t.Fatalf("非法项应被跳过，实际 %d 段", len(ws))
	}
	if !InFreeWindow(ws, 1*60+30) {
		t.Error("合法项 01:00-02:00 必须保留")
	}
}

// 移植语义锁：start ∈ [0,1439]，end ≤ 1440——"00:00-24:00" 作全天窗口合法
// （Python t38 用它做测试锚点；首版 Go 解析拒了 24:00 被状态机测试当场抓住）。
func TestParseFreeWindowsEndOfDay24(t *testing.T) {
	ws := ParseFreeWindows("00:00-24:00")
	if len(ws) != 1 {
		t.Fatalf("24:00 作为结束必须合法，实际 %d 段", len(ws))
	}
	if !InFreeWindow(ws, 12*60) {
		t.Error("00:00-24:00 应覆盖全天")
	}
}

// 行为：面板提交的免认证时段必须严格校验——非法值不能被静默跳过（那会让
// 用户以为设置生效、实际夜间照常登录）。空串是合法的「禁用」。
func TestValidateFreeWindows(t *testing.T) {
	valid := []string{"", "23:30-06:30", "00:00-24:00", "23:30-06:30,12:00-12:30", " 23:30 - 06:30 "}
	for _, s := range valid {
		if err := ValidateFreeWindows(s); err != nil {
			t.Errorf("%q 应合法，实际 %v", s, err)
		}
	}
	invalid := []string{
		"23:30",
		"25:00-06:30",
		"23:30-25:00",
		"23:30-23:30",
		"23:30-06:30,",
		"abc-def",
		"24:00-06:30",
		"23:30-06:30,,12:00-12:30",
	}
	for _, s := range invalid {
		if err := ValidateFreeWindows(s); err == nil {
			t.Errorf("%q 必须被拒绝", s)
		}
	}
}

// 行为：面板保存后免认证时段立即生效（不重启）——SetFreeWindows 换掉窗口，
// 旧的窗口不再命中。
func TestSetFreeWindowsHotReload(t *testing.T) {
	d := NewDaemon("")
	d.SetFreeWindows("00:00-24:00")
	if !InFreeWindow(d.FreeWindows, 12*60) {
		t.Error("设置全天窗口后 12:00 应命中")
	}
	if d.FreeWindowsText() != "00:00-24:00" {
		t.Errorf("面板回显应拿到新原文，实际 %q", d.FreeWindowsText())
	}
	d.SetFreeWindows("")
	if InFreeWindow(d.FreeWindows, 12*60) {
		t.Error("清空后不应再命中（禁用）")
	}
}
