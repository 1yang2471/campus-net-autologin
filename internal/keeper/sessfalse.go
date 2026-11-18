package keeper

import "regexp"

// alreadyOnline 「已在线」类成功语义（重复登录不算失败）——Python P-3 锚定
// 匹配移植：负向形态（不在线/未在线）先排除，再认两种锚定正向。
func alreadyOnline(msg string) bool {
	if reNegativeOnline.MatchString(msg) {
		return false
	}
	return reAlreadyOnline.MatchString(msg)
}

var (
	reNegativeOnline = regexp.MustCompile(`[不未]在线`)
	reAlreadyOnline  = regexp.MustCompile(`已在线|在线\s*[，,]?\s*(请勿|重复)`)
)
