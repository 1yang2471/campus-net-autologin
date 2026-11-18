package main

import (
	"sync"
)

// 图标缓存：托盘状态在两态间抖动（离线↔在线）时避免每次重新画 32×32 SDF +
// PNG 编码。键 = (color, shape)；值 = ICO 字节。条目数上限 = 状态数 × 形状数
// （个位数），不做淘汰。键拼接用 "|"：colorHex 经 parseHexColor 限 #rrggbb、
// shape 是枚举，都不含 "|"（校验在 get 内，不依赖调用方）。
type iconCache struct {
	mu   sync.RWMutex
	byKV map[string][]byte
}

func newIconCache() *iconCache {
	return &iconCache{byKV: map[string][]byte{}}
}

func iconCacheKey(colorHex, shape string) string { return colorHex + "|" + shape }

// get 返回缓存的 ICO 字节；未命中则生成并缓存。颜色先经 parseHexColor
// 校验（约束收在缓存内，不依赖调用方传词表值），未知形状/坏颜色不进缓存
// （失败可观测，调用方按既有纪律把失败写状态日志）。
func (c *iconCache) get(colorHex, shape string) ([]byte, error) {
	if _, err := parseHexColor(colorHex); err != nil {
		return nil, err
	}
	key := iconCacheKey(colorHex, shape)
	c.mu.RLock()
	ico, ok := c.byKV[key]
	c.mu.RUnlock()
	if ok {
		return ico, nil
	}
	ico, err := stateIconShape(colorHex, shape)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// 锁内直接覆盖（stateIconShape 是纯函数，同键字节相同，谁先谁后无差异）。
	c.byKV[key] = ico
	return ico, nil
}

func (c *iconCache) size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.byKV)
}
