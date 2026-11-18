package main

// exe 图标生成（构建机工具，不随产品发布）：
//
//	go run ./cmd/cnalw/genicon     # 产出 cmd/cnalw/app.ico
//	rsrc -ico cmd/cnalw/app.ico -arch amd64 -o cmd/cnalw/rsrc_windows_amd64.syso
//
// 图标与托盘状态图标同一绘制法（实心圆点 accent 绿），ICO 里放 16/32/48/256
// 四档。syso 随构建自动链接进 exe（资源管理器/任务栏/开始菜单显示）。
// 版本信息（FileDescription 等）由 rsrc -manifest 或 -version 单独补——
// 当前只有图标，资源管理器列表里显示文件名即可（de-slop：不造无关元数据）。

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

func main() {
	sizes := []int{16, 32, 48, 256}
	// 先生成全部 PNG，尺寸表才填得准。
	pngs := make([][]byte, len(sizes))
	for i, s := range sizes {
		pngs[i] = icoPng(s)
	}
	var buf bytes.Buffer
	// ICONDIR: reserved=0, type=1(icon), count=N
	writeLE(&buf, uint16(0), uint16(1), uint16(len(sizes)))
	offset := 6 + 16*len(sizes)
	for i, s := range sizes {
		// ICONDIRENTRY: w, h, colors, reserved, planes, bpp, bytesInRes, offset
		writeLE(&buf, uint8(s%256), uint8(s%256), uint8(0), uint8(0),
			uint16(1), uint16(32), uint32(len(pngs[i])), uint32(offset))
		offset += len(pngs[i])
	}
	for _, p := range pngs {
		buf.Write(p)
	}
	if err := os.WriteFile("cmd/cnalw/app.ico", buf.Bytes(), 0o644); err != nil {
		panic(err)
	}
}

// writeLE 逐字段小端写入（binary.Write 单字段形态， variadic 不可用）。
func writeLE(buf *bytes.Buffer, fields ...any) {
	for _, f := range fields {
		_ = binary.Write(buf, binary.LittleEndian, f)
	}
}

func icoPng(size int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	cx := float64(size) / 2
	r := cx * 0.75
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cx
			d := math.Sqrt(dx*dx+dy*dy) - r
			a := uint8(0)
			if d <= -1 {
				a = 255
			} else if d < 0 {
				a = uint8(255 * -d)
			}
			if a > 0 {
				img.SetRGBA(x, y, color.RGBA{R: 79, G: 157, B: 99, A: a}) // #4f9d63
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}
