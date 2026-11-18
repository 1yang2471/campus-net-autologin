package keeper

import (
	"regexp"
	"strconv"
	"time"
)

// reFreezeSeconds 从冻结文案解析剩余秒数（Python re.search(r"剩余\s*(\d+)\s*s")）。
var reFreezeSeconds = regexp.MustCompile(`剩余\s*(\d+)\s*s`)

// freezeWait 判门户拒绝消息是否为限频冻结，并给出冻结时长：
// max(floor, 剩余秒数+15)——floor 取 reject_backoff（可配，默认 600s）；
// 剩余秒数缺失或无标记时用 floor 兜底。
// Python handle_rejection「冻结风暴封堵」同源：冻结闸独立于普通退避，
// 锚点抖动/flux 复活都不得提前放行，否则越撞越深。
func freezeWait(msg string, marks ResponseMarks, floor time.Duration) (bool, time.Duration) {
	if !marks.withDefaults().freezeMarked(msg) {
		return false, 0
	}
	w := floor
	if w <= 0 {
		w = defaultRejectBackoff
	}
	if m := reFreezeSeconds.FindStringSubmatch(msg); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			if d := time.Duration(n+15) * time.Second; d > w {
				w = d
			}
		}
	}
	return true, w
}
