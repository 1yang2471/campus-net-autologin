package keeper

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"cnal/internal/dialect"
)

// 登录响应判定（Python Keeper.parse_login 同语义）：标准 JSON / 华为伪 JSON /
// ret_code 型 / 纯文本多形态分治。判定结构固定，异校只换词表（[response]）。
// 关键护栏：负向词只作用于 ≤200 字符短响应——HTML 门户页里常有「错误提示/
// onerror」字样，一刀切会把成功页误判成硬拒绝（600s 最重惩罚）；长响应只认
// 强正向标记，判不了归「无法判定」→ 瞬态短退避。
const shortResponseLimit = 200

var (
	reSuccessWord  = regexp.MustCompile(`(?i)\bsuccess`)
	reLoginOKFalse = regexp.MustCompile(`(?i)login_ok\s*=\s*false`)
	reLoginOKTrue  = regexp.MustCompile(`(?i)login_ok\s*=\s*true`)
	// Python parse_login 第 2 段：华为伪 JSON 用正则从任意长度的正文里提
	// 'success'/'msg'（容忍单/双引号与内嵌引号），不要求整体是字典。
	// Go RE2 不支持反向引用 \1，按值的单/双引号拆成两个正则（Python
	// 的 (['"])(.*?)\1 同义）。
	rePseudoSuccess = regexp.MustCompile(`(?i)'success'\s*:\s*(true|false)`)
	rePseudoMsgSQ   = regexp.MustCompile(`(?i)'msg'\s*:\s*'(.*?)'\s*[,}]`)
	rePseudoMsgDQ   = regexp.MustCompile(`(?i)'msg'\s*:\s*"(.*?)"\s*[,}]`)
)

// JudgeLoginResponse 判定门户登录应答：成功返回响应原文（保留「已在线」哨兵，
// daemon 靠它触发 SESSION_FALSE 自停闸）；失败返回 error，错误文本带门户
// 原因（daemon 用 transient/freeze 词表再分类退避时长）。
func JudgeLoginResponse(resp string, marks ResponseMarks) (string, error) {
	t := strings.TrimSpace(resp)
	if t == "" {
		return "", errors.New("登录未受理: 空响应")
	}
	m := marks.withDefaults()
	// Python 分治顺序：标准 JSON / 华为伪 JSON 都以 "{" 开头，才走结构键判定；
	// 长 HTML 里若只是碰巧出现 "success":false 片段，不能让方言解析器把它
	// 当门户应答（否则长响应护栏失效，成功页被判硬拒绝 → 600s 重罚）。
	if strings.HasPrefix(t, "{") {
		if data, err := dialect.Parse(t); err == nil {
			if out, jerr, handled := judgeDict(data, m, t); handled {
				return out, jerr
			}
		}
	}
	// 华为伪 JSON 正则兜底（Python 第 2 段同序）：包在 HTML/额外文本里的
	// 'success':false + 'msg' 也按结构判定，msg 里的「已在线」仍是成功哨兵。
	if pm := rePseudoSuccess.FindStringSubmatch(t); pm != nil {
		if strings.EqualFold(pm[1], "true") {
			return t, nil
		}
		msg := ""
		if mm := rePseudoMsgSQ.FindStringSubmatch(t); mm != nil {
			msg = truncRunes(mm[1], 120)
		} else if mm := rePseudoMsgDQ.FindStringSubmatch(t); mm != nil {
			msg = truncRunes(mm[1], 120)
		}
		if alreadyOnline(msg) {
			return t, nil
		}
		return "", fmt.Errorf("登录未受理: %s", msgOr(msg, "服务器拒绝"))
	}
	return judgeText(t, m)
}

// judgeDict 标准 JSON / 伪 JSON 的键判定。handled=false 表示响应里没有
// success/ret_code/code 这类结构键，交给纯文本兜底。
func judgeDict(data map[string]any, m ResponseMarks, raw string) (string, error, bool) {
	if v, has := data["success"]; has {
		if isTrueValue(v) {
			return raw, nil, true
		}
		msg := responseMsg(data)
		if alreadyOnline(msg) {
			return raw, nil, true // 「已在线」哨兵：重复登录不算失败
		}
		return "", fmt.Errorf("登录未受理: %s",
			truncRunes(msgOr(msg, "服务器拒绝"), 120)), true
	}
	codeVal, has := data["ret_code"]
	if !has {
		codeVal, has = data["code"]
	}
	if has {
		code := fmt.Sprint(codeVal)
		msg := responseMsg(data)
		if code == "0" || m.successMarked(msg) {
			return raw, nil, true
		}
		if alreadyOnline(msg) {
			return raw, nil, true
		}
		return "", fmt.Errorf("登录未受理: %s", msgOr(msg, truncRunes(raw, 120))), true
	}
	return "", nil, false
}

// judgeText 纯文本兜底（Python parse_login 第 3 段）：短响应认负向词与
// login_ok/success 标记；长响应只认强正向标记，判不了归「无法判定」。
func judgeText(t string, m ResponseMarks) (string, error) {
	// Python len() 是码点口径：中文短响应不得因为 UTF-8 字节多被误归「长响应」
	// （否则负向词护栏失效，硬拒绝退化成无法判定）。
	if len([]rune(t)) <= shortResponseLimit {
		if m.negativeMarked(t) || reLoginOKFalse.MatchString(t) {
			return "", fmt.Errorf("登录未受理: %s", truncRunes(t, 120))
		}
		if m.successMarked(t) || reSuccessWord.MatchString(t) {
			return t, nil
		}
		return "", fmt.Errorf("登录未受理: %s", truncRunes(t, 120))
	}
	if reLoginOKFalse.MatchString(t) {
		return "", fmt.Errorf("登录未受理: %s", truncRunes(t, 120))
	}
	if reLoginOKTrue.MatchString(t) || m.successMarked(t) {
		return t, nil
	}
	return "", errors.New("登录未受理: 无法判定响应内容")
}

// isTrueValue success 字段的正向形态（Python：True/"true"/1/"1"）。
func isTrueValue(v any) bool {
	switch tv := v.(type) {
	case bool:
		return tv
	case string:
		return tv == "true" || tv == "1"
	}
	return false
}

// responseMsg 门户消息字段（msg 优先，message 兜底；Python 同序）。
func responseMsg(data map[string]any) string {
	for _, k := range []string{"msg", "message"} {
		if v, ok := data[k]; ok && v != nil {
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" {
				return s
			}
		}
	}
	return ""
}

func msgOr(msg, fallback string) string {
	if strings.TrimSpace(msg) != "" {
		return strings.TrimSpace(msg)
	}
	return fallback
}

// truncRunes 按字符截断（门户原文可能含多字节中文；Python t[:120] 是码点口径）。
func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
