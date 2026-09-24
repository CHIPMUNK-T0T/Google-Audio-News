package media

import (
	_ "embed"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg" // allow a JPEG background
	"image/png"
	"io/fs"
	"os"
	"time"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Size of the rendered video. 480p is enough for a still image and keeps
// the file small.
const (
	VideoWidth  = 854
	VideoHeight = 480
)

// dateFont is Noto Sans JP Bold reduced to the glyphs a date needs (see
// fonts/OFL.txt for its license). Characters outside dateChars are not in it.
// To add characters, rebuild it from NotoSansJP-Bold.otf (notofonts/noto-cjk,
// Sans/SubsetOTF/JP) with:
//
//	pyftsubset NotoSansJP-Bold.otf --text=<dateChars> --layout-features='' \
//	    --no-hinting --desubroutinize --output-file=NotoSansJP-Bold-date.otf
//
//go:embed fonts/NotoSansJP-Bold-date.otf
var dateFont []byte

const dateChars = "0123456789年月日曜火水木金土"

var weekdays = [...]string{"日", "月", "火", "水", "木", "金", "土"}

// dateLine is one line of the date, placed relative to the frame size so it
// lands in the dark area at the top right of assets/background.png.
type dateLine struct {
	text     string
	size     float64 // font size as a fraction of the frame height
	baseline float64 // baseline y as a fraction of the frame height
}

// dateCenterX is the horizontal centre of the date as a fraction of the
// frame width.
const dateCenterX = 0.804

func dateLines(date time.Time) []dateLine {
	return []dateLine{
		{fmt.Sprintf("%d年%d月%d日", date.Year(), date.Month(), date.Day()), 0.073, 0.175},
		{weekdays[date.Weekday()] + "曜日", 0.055, 0.265},
	}
}

// RenderFrame scales the background at imagePath to the video size, draws
// date on it (for example "2026年9月24日" and "木曜日") and writes the result
// to output as PNG. The background is cropped around its centre to 16:9. If
// imagePath does not exist, a plain dark background is used.
func RenderFrame(imagePath string, date time.Time, output string) error {
	frame := image.NewRGBA(image.Rect(0, 0, VideoWidth, VideoHeight))
	src, err := loadImage(imagePath)
	switch {
	case err == nil:
		draw.CatmullRom.Scale(frame, frame.Bounds(), src, cropToFrame(src.Bounds()), draw.Src, nil)
	case errors.Is(err, fs.ErrNotExist):
		draw.Draw(frame, frame.Bounds(), image.NewUniform(color.RGBA{0x14, 0x21, 0x3d, 0xff}), image.Point{}, draw.Src)
	default:
		return err
	}

	f, err := opentype.Parse(dateFont)
	if err != nil {
		return fmt.Errorf("parse date font: %w", err)
	}
	for _, line := range dateLines(date) {
		face, err := opentype.NewFace(f, &opentype.FaceOptions{
			Size:    line.size * VideoHeight,
			DPI:     72,
			Hinting: font.HintingNone,
		})
		if err != nil {
			return fmt.Errorf("date font face: %w", err)
		}
		d := &font.Drawer{Dst: frame, Src: image.White, Face: face}
		width := d.MeasureString(line.text)
		d.Dot = fixed.Point26_6{
			X: toFixed(dateCenterX*VideoWidth) - width/2,
			Y: toFixed(line.baseline * VideoHeight),
		}
		d.DrawString(line.text)
		face.Close()
	}

	out, err := os.Create(output)
	if err != nil {
		return err
	}
	if err := png.Encode(out, frame); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func toFixed(px float64) fixed.Int26_6 {
	return fixed.Int26_6(px * 64)
}

func loadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return img, nil
}

// cropToFrame returns the largest centred part of b with the frame's aspect
// ratio.
func cropToFrame(b image.Rectangle) image.Rectangle {
	w, h := b.Dx(), b.Dy()
	if w*VideoHeight > h*VideoWidth {
		cw := h * VideoWidth / VideoHeight
		b.Min.X += (w - cw) / 2
		b.Max.X = b.Min.X + cw
	} else {
		ch := w * VideoHeight / VideoWidth
		b.Min.Y += (h - ch) / 2
		b.Max.Y = b.Min.Y + ch
	}
	return b
}
