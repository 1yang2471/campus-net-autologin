package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"strconv"
)

// 托盘图标：状态色圆点 ICO，PNG-in-ICO 封装（Vista+ 支持），纯 stdlib——
// 托盘不引图像库。色值来自 internal/api 词表（与 Python 守护同源）。

// iconSize 图标边长（像素）。
const iconSize = 32

// stateIcon 生成状态色实心圆点 ICO（旧签名，等价 solid 形状）。
func stateIcon(colorHex string) ([]byte, error) {
	return stateIconShape(colorHex, "solid")
}

// stateIconShape 按形状生成状态图标（规格 §13.2：颜色不得是唯一载体）：
// solid=实心圆（在线）、ring=圆环（离线/门户不可达）、ring_slash=圆环+斜杠
// （登录被拒）、lamp=环+心点（免认证/暂停/启动中，Python 守护状态灯同款）。
func stateIconShape(colorHex, shape string) ([]byte, error) {
	c, err := parseHexColor(colorHex)
	if err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, iconSize, iconSize))
	cx, cy := float64(iconSize)/2, float64(iconSize)/2
	// 距离场抗锯齿：alpha 沿边缘 1px 渐变（与原实心圆同法）。
	sdf := func(x, y float64) float64 {
		dx, dy := x+0.5-cx, y+0.5-cy
		return math.Sqrt(dx*dx + dy*dy)
	}
	coverage := func(dist, outer, inner float64) uint8 {
		switch {
		case dist > outer+1:
			return 0
		case dist > outer:
			return uint8(255 * (outer + 1 - dist))
		case inner >= 0 && dist < inner-1:
			return 0 // 环内镂空
		case inner >= 0 && dist < inner:
			return uint8(255 * (dist - inner + 1))
		default:
			return 255
		}
	}
	var draw func(x, y int) uint8
	switch shape {
	case "solid":
		draw = func(x, y int) uint8 { return coverage(sdf(float64(x), float64(y)), 12, -1) }
	case "lamp":
		// 空闲态=环+心点（Python 守护状态灯同款，app.py create_oval 画法）：
		// 小实心点在 16px 托盘缩放后仅 ~3px 灰点，肉眼近乎不可见
		// （2026-11-08 用户实证"啥都看不到"；判据见 TestIdleIconVisibleAtTraySize）。
		draw = func(x, y int) uint8 {
			d := sdf(float64(x), float64(y))
			if d <= 6 {
				if d > 5 {
					return uint8(255 * (6 - d))
				}
				return 255 // 心点
			}
			return coverage(d, 12, 8) // 环
		}
	case "ring":
		draw = func(x, y int) uint8 { return coverage(sdf(float64(x), float64(y)), 12, 8) }
	case "ring_slash":
		draw = func(x, y int) uint8 {
			px, py := float64(x)+0.5, float64(y)+0.5
			a := coverage(sdf(float64(x), float64(y)), 12, 8)
			// 斜杠线段 (8,24)-(24,8)（左下到右上），距线 1.2px 内覆盖。
			seg := slashDist(px, py, 8, 24, 24, 8)
			var s uint8
			switch {
			case seg > 1.2+1:
				s = 0
			case seg > 1.2:
				s = uint8(255 * (1.2 + 1 - seg))
			default:
				s = 255
			}
			if s > a {
				return s
			}
			return a
		}
	default:
		return nil, errUnknownShape
	}
	for y := 0; y < iconSize; y++ {
		for x := 0; x < iconSize; x++ {
			a := draw(x, y)
			if a == 0 {
				continue
			}
			img.SetRGBA(x, y, color.RGBA{R: c.R, G: c.G, B: c.B, A: a})
		}
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		return nil, err
	}
	return wrapIco(pngBuf.Bytes(), iconSize)
}

// slashDist 点到线段 (x1,y1)-(x2,y2) 的最短距离。
func slashDist(px, py, x1, y1, x2, y2 float64) float64 {
	dx, dy := x2-x1, y2-y1
	l2 := dx*dx + dy*dy
	t := ((px-x1)*dx + (py-y1)*dy) / l2
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	ox, oy := px-(x1+t*dx), py-(y1+t*dy)
	return math.Sqrt(ox*ox + oy*oy)
}

// errUnknownShape 未知形状（调用方决定是否致命）。
var errUnknownShape = fmt.Errorf("未知图标形状")

// wrapIco 把单帧 PNG 封进 ICO 容器（ICONDIR + ICONDIRENTRY + PNG）。
func wrapIco(pngData []byte, size int) ([]byte, error) {
	if size <= 0 || size > 255 {
		return nil, fmt.Errorf("图标尺寸必须 1-255: %d", size)
	}
	var buf bytes.Buffer
	hdr := make([]byte, 6)
	binary.LittleEndian.PutUint16(hdr[2:], 1) // type=icon
	binary.LittleEndian.PutUint16(hdr[4:], 1) // count=1
	buf.Write(hdr)
	entry := make([]byte, 16)
	entry[0] = byte(size)
	entry[1] = byte(size)
	binary.LittleEndian.PutUint16(entry[4:], 1)  // planes
	binary.LittleEndian.PutUint16(entry[6:], 32) // bpp
	binary.LittleEndian.PutUint32(entry[8:], uint32(len(pngData)))
	binary.LittleEndian.PutUint32(entry[12:], 22) // offset
	buf.Write(entry)
	buf.Write(pngData)
	return buf.Bytes(), nil
}

// parseHexColor 解析 #rrggbb。
func parseHexColor(s string) (color.RGBA, error) {
	if len(s) != 7 || s[0] != '#' {
		return color.RGBA{}, fmt.Errorf("色值必须是 #rrggbb: %q", s)
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return color.RGBA{}, fmt.Errorf("色值必须是 #rrggbb: %q", s)
	}
	return color.RGBA{
		R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255,
	}, nil
}
