package keeper

import "strings"

// ResponseMarks 登录响应判定词表（Python RESPONSE_DEFAULTS 同语义）：
// 异校在 config.ini 的 [response] 段覆盖，未配置的字段回落默认值。
// 中文强标记走词表，英文正向走 \bsuccess 正则（防 "unsuccessful" 误命中，
// Python trouble-maker P1-9 护栏保留）。
type ResponseMarks struct {
	Success   []string // success_marks：正向强标记
	Negative  []string // negative_marks：短响应里的负向词
	Transient []string // transient_marks：瞬态错误（短退避快速重试）
	Freeze    []string // freeze_marks：门户限频冻结（独立冻结闸）
}

// DefaultResponseMarks 参考校/华为默认词表，与 Python RESPONSE_DEFAULTS 逐字
// 同源；「不含裸 success」的护栏也一并继承（英文正向只认 \bsuccess）。
func DefaultResponseMarks() ResponseMarks {
	return ResponseMarks{
		Success:   SplitMarks("成功,登录成功,login_ok"),
		Negative:  SplitMarks("未成功,不成功,失败,错误,error,fail"),
		Transient: SplitMarks("noauth,not taken,请求异常,空响应,timeout,timed out,无法判定"),
		Freeze:    SplitMarks("冻结,freeze"),
	}
}

// SplitMarks 按逗号切词表并去空白（空项丢弃；与 Python _marks 同一口径）。
func SplitMarks(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// withDefaults 空字段回落默认词表（Python _marks：cfg 值为空即用默认值，
// 因此「显式清空某组词表」等于恢复默认，而不是禁用判定）。
func (m ResponseMarks) withDefaults() ResponseMarks {
	d := DefaultResponseMarks()
	if len(m.Success) == 0 {
		m.Success = d.Success
	}
	if len(m.Negative) == 0 {
		m.Negative = d.Negative
	}
	if len(m.Transient) == 0 {
		m.Transient = d.Transient
	}
	if len(m.Freeze) == 0 {
		m.Freeze = d.Freeze
	}
	return m
}

// marked 词表命中（大小写不敏感——英文词表在门户响应里大小写不定）。
func marked(marks []string, s string) bool {
	low := strings.ToLower(s)
	for _, mark := range marks {
		if strings.Contains(low, strings.ToLower(mark)) {
			return true
		}
	}
	return false
}

func (m ResponseMarks) successMarked(s string) bool   { return marked(m.Success, s) }
func (m ResponseMarks) negativeMarked(s string) bool  { return marked(m.Negative, s) }
func (m ResponseMarks) transientMarked(s string) bool { return marked(m.Transient, s) }
func (m ResponseMarks) freezeMarked(s string) bool    { return marked(m.Freeze, s) }
