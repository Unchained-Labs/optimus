package qrcode

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/makiuchi-d/gozxing"
	gzqr "github.com/makiuchi-d/gozxing/qrcode"
)

// TestTerminalScans rasterizes the half-block output the way a dark terminal
// draws it (blocks light, spaces dark) and decodes it with an independent
// QR decoder.
func TestTerminalScans(t *testing.T) {
	want := "http://100.64.0.1:7777/?token=0123456789abcdef0123456789abcdef"
	s, err := Terminal(want)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	const px = 6
	w, h := len([]rune(lines[0]))*px, len(lines)*2*px
	img := image.NewGray(image.Rect(0, 0, w, h))
	for row, l := range lines {
		for col, r := range []rune(l) {
			top, bot := r == '█' || r == '▀', r == '█' || r == '▄'
			for dy := 0; dy < 2*px; dy++ {
				on := top
				if dy >= px {
					on = bot
				}
				c := color.Gray{0}
				if on {
					c = color.Gray{255}
				}
				for dx := 0; dx < px; dx++ {
					img.SetGray(col*px+dx, row*2*px+dy, c)
				}
			}
		}
	}
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		t.Fatal(err)
	}
	res, err := gzqr.NewQRCodeReader().Decode(bmp, nil)
	if err != nil {
		t.Fatalf("terminal QR doesn't scan: %v", err)
	}
	if res.GetText() != want {
		t.Fatalf("decoded %q", res.GetText())
	}
}
