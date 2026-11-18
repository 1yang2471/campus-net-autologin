package main

import (
	"sync"
	"testing"
)

// 图标缓存：同 (color, shape) 只生成一次 ICO（状态在两态间抖动时避免反复
// 编码 PNG）。缓存命中必须与首次生成字节一致。

func TestStateIconShapeCacheHits(t *testing.T) {
	cache := newIconCache()
	a, err := cache.get("#4f9d63", "solid")
	if err != nil {
		t.Fatal(err)
	}
	b, err := cache.get("#4f9d63", "solid")
	if err != nil {
		t.Fatal(err)
	}
	if &a[0] == &b[0] {
		// 允许同一底层数组返回两次——命中缓存的标志恰恰是共享。
		t.Log("两次返回共享底层数组（缓存命中）")
	}
	if string(a) != string(b) {
		t.Error("缓存命中应返回相同字节")
	}
	if cache.size() != 1 {
		t.Errorf("同色同形应只生成一次，实际缓存 %d 项", cache.size())
	}
}

func TestStateIconShapeCacheDistinguishesInputs(t *testing.T) {
	cache := newIconCache()
	solid, _ := cache.get("#4f9d63", "solid")
	ring, _ := cache.get("#4f9d63", "ring")
	if string(solid) == string(ring) {
		t.Error("不同形状不得共享缓存项")
	}
	other, _ := cache.get("#d1913f", "solid")
	if string(solid) == string(other) {
		t.Error("不同颜色不得共享缓存项")
	}
	if cache.size() != 3 {
		t.Errorf("三种输入应产生 3 项缓存，实际 %d", cache.size())
	}
}

// 并发安全：tick 循环是唯一写者，但缓存仍以并发安全实现（RWMutex），
// 防未来第二调用方踩竞态。
func TestIconCacheConcurrent(t *testing.T) {
	cache := newIconCache()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, s := range []string{"solid", "ring", "lamp", "ring_slash"} {
				if _, err := cache.get("#4f9d63", s); err != nil {
					t.Errorf("并发生成 %s 失败: %v", s, err)
				}
			}
		}()
	}
	wg.Wait()
	if cache.size() != 4 {
		t.Errorf("并发后应恰 4 项，实际 %d", cache.size())
	}
}

// 未知形状必须报错且不进缓存（失败可观测，不留空项）。
func TestIconCacheUnknownShapeNotCached(t *testing.T) {
	cache := newIconCache()
	if _, err := cache.get("#4f9d63", "bogus"); err == nil {
		t.Error("未知形状必须报错")
	}
	if cache.size() != 0 {
		t.Errorf("失败项不得进缓存，实际 %d", cache.size())
	}
}

// 坏颜色在缓存入口就拒绝（约束收在缓存内，不依赖调用方传词表值）。
func TestIconCacheRejectsBadColor(t *testing.T) {
	cache := newIconCache()
	if _, err := cache.get("not-a-color", "solid"); err == nil {
		t.Error("坏颜色必须报错")
	}
	if _, err := cache.get("#4f9d63|ring", "solid"); err == nil {
		t.Error("含 | 的颜色必须拒绝（键拼接碰撞防护）")
	}
	if cache.size() != 0 {
		t.Errorf("失败项不得进缓存，实际 %d", cache.size())
	}
}
