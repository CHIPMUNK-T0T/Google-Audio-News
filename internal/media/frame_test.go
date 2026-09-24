package media

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/image/font/sfnt"
)

func TestDateFontHasEveryGlyph(t *testing.T) {
	f, err := sfnt.Parse(dateFont)
	if err != nil {
		t.Fatal(err)
	}
	var buf sfnt.Buffer
	for _, r := range dateChars {
		if i, err := f.GlyphIndex(&buf, r); err != nil || i == 0 {
			t.Errorf("date font has no glyph for %q", r)
		}
	}

	// Every date must only use characters in the font.
	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 7; i++ {
		for _, line := range dateLines(day.AddDate(0, 0, i)) {
			for _, r := range line.text {
				if !strings.ContainsRune(dateChars, r) {
					t.Errorf("%q uses %q, which is not in dateChars", line.text, r)
				}
			}
		}
	}
}

func TestDateLines(t *testing.T) {
	lines := dateLines(time.Date(2026, 9, 24, 8, 15, 0, 0, time.UTC))
	if lines[0].text != "2026年9月24日" || lines[1].text != "木曜日" {
		t.Errorf("date lines = %q, %q", lines[0].text, lines[1].text)
	}
}

func TestCropToFrame(t *testing.T) {
	for _, tc := range []struct {
		in, want image.Rectangle
	}{
		{image.Rect(0, 0, 1672, 941), image.Rect(0, 1, 1672, 940)},
		{image.Rect(0, 0, 2000, 480), image.Rect(573, 0, 1427, 480)},
		{image.Rect(0, 0, 854, 480), image.Rect(0, 0, 854, 480)},
	} {
		if got := cropToFrame(tc.in); got != tc.want {
			t.Errorf("cropToFrame(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestRenderFrame(t *testing.T) {
	dir := t.TempDir()
	date := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	for name, background := range map[string]string{
		"with image":    filepath.Join("..", "..", "assets", "background.png"),
		"missing image": filepath.Join(dir, "missing.png"),
	} {
		out := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".png")
		if err := RenderFrame(background, date, out); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		f, err := os.Open(out)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if b := img.Bounds(); b.Dx() != VideoWidth || b.Dy() != VideoHeight {
			t.Errorf("%s: frame size = %v", name, b.Size())
		}

		// The date is white text around the top right of the frame.
		white := 0
		for y := VideoHeight / 10; y < VideoHeight*3/10; y++ {
			for x := VideoWidth * 65 / 100; x < VideoWidth*95/100; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				if r > 0xf000 && g > 0xf000 && b > 0xf000 {
					white++
				}
			}
		}
		if white < 500 {
			t.Errorf("%s: found %d white pixels where the date should be", name, white)
		}
	}
}

func TestRenderFrameBadImage(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.png")
	if err := os.WriteFile(bad, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RenderFrame(bad, time.Now(), filepath.Join(dir, "out.png")); err == nil {
		t.Error("RenderFrame accepted a broken image")
	}
}
