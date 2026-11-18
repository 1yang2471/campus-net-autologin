package main

import (
	"bytes"
	"image/png"
	"testing"
)

// UI③形状区分（规格 §13.2：颜色不得是唯一载体）——四态图标各有形状：
// online=实心圆、offline/portal_down=圆环、rejected=圆环+斜杠、
// free_window/paused/starting=环+心点（Python 守护状态灯同款）。

// 行为：stateIconShape 按形状产出可解码 PNG；像素覆盖率单调可分
// （2026-11-08 实测：实心 > 心点灯 > 斜杠环 > 圆环 > 小点——斜杠 2.4px
// 细线增量小于心点 r6 圆面，单调方向以实测为准不凭直觉）。
func TestIconShapesAreDistinct(t *testing.T) {
	count := func(shape string) int {
		ico, err := stateIconShape("#4f9d63", shape)
		if err != nil {
			t.Fatalf("%s 生成失败: %v", shape, err)
		}
		img, err := png.Decode(bytes.NewReader(ico[22:]))
		if err != nil {
			t.Fatalf("%s 解码失败: %v", shape, err)
		}
		n := 0
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
					n++
				}
			}
		}
		return n
	}
	solid := count("solid")
	ring := count("ring")
	slash := count("ring_slash")
	lamp := count("lamp")
	if solid <= lamp {
		t.Errorf("实心(%d)必须比心点灯(%d)像素多", solid, lamp)
	}
	if ring >= slash {
		t.Errorf("斜杠环(%d)必须比净圆环(%d)像素多（斜杠是加法）", slash, ring)
	}
	if lamp <= slash {
		t.Errorf("心点灯(%d)必须比斜杠环(%d)像素多（心点 r6 圆面 > 2.4px 斜杠线）", lamp, slash)
	}
}

// 行为：空闲态（免认证/暂停/启动中）图标在 16px 托盘尺寸下肉眼可见——
// 判据：画布覆盖率 ≥ 25%（2026-11-08 实证：6px 小灰点 = 11% 覆盖率，
// 用户报告"啥都看不到"）。
func TestIdleIconVisibleAtTraySize(t *testing.T) {
	ico, err := stateIconShape("#8b919b", "lamp")
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(ico[22:]))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
				n++
			}
		}
	}
	total := (b.Max.X - b.Min.X) * (b.Max.Y - b.Min.Y)
	if coverage := n * 100 / total; coverage < 25 {
		t.Errorf("空闲态图标覆盖率 %d%% < 25%%，16px 托盘上不可见", coverage)
	}
}

// 行为：未知形状 → 明确报错（不静默退回实心）。
func TestIconShapeRejectsUnknown(t *testing.T) {
	if _, err := stateIconShape("#4f9d63", "star"); err == nil {
		t.Fatal("未知形状必须报错")
	}
}

// 行为：旧签名 stateIcon(色) 等价于 stateIconShape(色, "solid")（兼容存量测试）。
func TestLegacyStateIconIsSolid(t *testing.T) {
	a, err := stateIcon("#4f9d63")
	if err != nil {
		t.Fatal(err)
	}
	b, err := stateIconShape("#4f9d63", "solid")
	if err != nil {
		t.Fatal(err)
	}
	if !imageOpaquesEqual(a, b) {
		t.Error("stateIcon 应与 solid 形状逐字节一致")
	}
}

func imageOpaquesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
