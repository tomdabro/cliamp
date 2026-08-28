package coverart

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHalfBlocksDimensions(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			img.Set(x, y, color.RGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}

	got := HalfBlocks(img, 4, 3)
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("line count = %d, want 3", len(lines))
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w != 4 {
			t.Errorf("line %d visible width = %d, want 4", i, w)
		}
		if !strings.HasSuffix(line, "\x1b[0m") {
			t.Errorf("line %d missing SGR reset", i)
		}
	}
}

func TestHalfBlocksColorsTopAndBottom(t *testing.T) {
	// One cell = one column, two vertical pixels: top red, bottom blue.
	img := image.NewRGBA(image.Rect(0, 0, 1, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(0, 1, color.RGBA{B: 255, A: 255})

	got := HalfBlocks(img, 1, 1)
	if !strings.Contains(got, "38;2;255;0;0") {
		t.Errorf("foreground (top pixel) not red: %q", got)
	}
	if !strings.Contains(got, "48;2;0;0;255") {
		t.Errorf("background (bottom pixel) not blue: %q", got)
	}
	if !strings.Contains(got, "▀") {
		t.Errorf("missing half-block rune: %q", got)
	}
}

func TestHalfBlocksEmpty(t *testing.T) {
	if got := HalfBlocks(nil, 4, 4); got != "" {
		t.Errorf("nil image = %q, want empty", got)
	}
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	if got := HalfBlocks(img, 0, 4); got != "" {
		t.Errorf("zero cols = %q, want empty", got)
	}
}

func TestImageFromFileURL(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 6, 4))
	for y := range 4 {
		for x := range 6 {
			src.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "cover.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, src); err != nil {
		t.Fatal(err)
	}
	f.Close()

	fileURL := (&url.URL{Scheme: "file", Path: path}).String()
	img, err := Image(context.Background(), fileURL)
	if err != nil {
		t.Fatalf("Image() error: %v", err)
	}
	if got := img.Bounds().Dx(); got != 6 {
		t.Errorf("decoded width = %d, want 6", got)
	}
}

func TestImageEmptyURL(t *testing.T) {
	if _, err := Image(context.Background(), ""); err == nil {
		t.Error("Image(\"\") = nil error, want error")
	}
}
