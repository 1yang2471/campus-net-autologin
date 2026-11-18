// Package dialect 解析门户的伪 JSON 方言：单引号键值、冒号后可带空格。
// 实测（2026-11-06 汕大）响应形如 {'success':true, 'ip':'…', 'isOnline':false}，
// 不是合法 JSON；判定结构固定，异校只换词表（规格 §4）。
package dialect

import (
	"strings"
)

// Parse 把方言字典拆成 map。非字典形态（HTML/空串）返回空 map 且不报错——
// 让调用方按"键缺失"降级，绝不把不可解析的东西编造成成功。
func Parse(s string) (map[string]any, error) {
	out := map[string]any{}
	body := strings.TrimSpace(s)
	body = strings.TrimPrefix(body, "{")
	body = strings.TrimSuffix(body, "}")
	if body == "" {
		return out, nil
	}
	for _, kv := range splitTopLevel(body) {
		k, v, found := strings.Cut(kv, ":")
		if !found {
			continue
		}
		key := unquote(strings.TrimSpace(k))
		val := coerce(strings.TrimSpace(v))
		if key == "" {
			continue
		}
		out[key] = val
	}
	return out, nil
}

// splitTopLevel 按逗号切，但跳过引号内的逗号（msg 里可能有中文逗号）。
func splitTopLevel(s string) []string {
	var parts []string
	var buf strings.Builder
	var q rune
	for _, r := range s {
		switch {
		case q != 0:
			buf.WriteRune(r)
			if r == q {
				q = 0
			}
		case r == '\'' || r == '"':
			q = r
			buf.WriteRune(r)
		case r == ',':
			parts = append(parts, buf.String())
			buf.Reset()
		default:
			buf.WriteRune(r)
		}
	}
	if strings.TrimSpace(buf.String()) != "" {
		parts = append(parts, buf.String())
	}
	return parts
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

func coerce(v string) any {
	switch v {
	case "true":
		return true
	case "false":
		return false
	case "null", "":
		return nil
	}
	return unquote(v)
}
