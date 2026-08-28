package coverart

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

func solidImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 120, G: 60, B: 200, A: 255})
		}
	}
	return img
}

// noiseImage produces a deterministic high-entropy image so its PNG encoding is
// large enough to force multi-chunk Kitty transmission.
func noiseImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	seed := uint32(2166136261)
	for y := range h {
		for x := range w {
			seed = seed*16777619 + uint32(x*31+y*7+1)
			img.Set(x, y, color.RGBA{R: byte(seed >> 16), G: byte(seed >> 8), B: byte(seed), A: 255})
		}
	}
	return img
}

func TestKittyTransmitStructure(t *testing.T) {
	// A large image forces multi-chunk transmission.
	seq, err := KittyTransmit(0x0A0B0C, noiseImage(200, 200), 14, 7, 0x010203)
	if err != nil {
		t.Fatalf("KittyTransmit error: %v", err)
	}
	// Deletes the previous image first.
	if !strings.Contains(seq, "a=d,d=I,i=66051") { // 0x010203
		t.Errorf("missing delete of previous image: %q", seq[:min(80, len(seq))])
	}
	// Transmits as PNG with the new id.
	if !strings.Contains(seq, "a=t,f=100,i=658188") { // 0x0A0B0C
		t.Errorf("missing transmit header with id: %q", seq[:min(120, len(seq))])
	}
	// Chunked: at least one continuation marker and a final m=0.
	if !strings.Contains(seq, "m=1") || !strings.Contains(seq, "m=0") {
		t.Errorf("expected chunked transmission markers (m=1/m=0)")
	}
	// Creates a Unicode-placeholder virtual placement sized to the cell box.
	if !strings.Contains(seq, "a=p,i=658188,U=1,c=14,r=7") {
		t.Errorf("missing virtual placement: %q", seq)
	}
}

func TestKittyTransmitChunkBounds(t *testing.T) {
	seq, err := KittyTransmit(1, noiseImage(300, 300), 20, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	// No delete when deletePrev == 0.
	if strings.Contains(seq, "a=d") {
		t.Errorf("unexpected delete sequence when deletePrev=0")
	}
	// Each APC payload chunk must stay within the 4096 base64-byte cap.
	for _, part := range strings.Split(seq, "\x1b_G") {
		if i := strings.IndexByte(part, ';'); i >= 0 {
			end := strings.Index(part, "\x1b\\")
			if end < 0 {
				continue
			}
			if payload := part[i+1 : end]; len(payload) > kittyChunk {
				t.Fatalf("chunk payload = %d bytes, want <= %d", len(payload), kittyChunk)
			}
		}
	}
}

func TestKittyPlaceholdersGrid(t *testing.T) {
	got := KittyPlaceholders(0x00FF80, 5, 3)
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("line count = %d, want 3", len(lines))
	}
	// Foreground SGR encodes the id bytes (0x00, 0xFF, 0x80).
	if !strings.Contains(got, "\x1b[38;2;0;255;128m") {
		t.Errorf("id color not encoded in foreground: %q", got)
	}
	for i, line := range lines {
		if n := strings.Count(line, string(kittyPlaceholderRune)); n != 5 {
			t.Errorf("line %d has %d placeholders, want 5", i, n)
		}
		if !strings.HasSuffix(line, "\x1b[0m") {
			t.Errorf("line %d missing SGR reset", i)
		}
	}
	// Row 0 uses the first row diacritic; row 1 the second.
	if !strings.ContainsRune(lines[0], kittyDiacritics[0]) {
		t.Error("row 0 missing first row diacritic")
	}
	if !strings.ContainsRune(lines[1], kittyDiacritics[1]) {
		t.Error("row 1 missing second row diacritic")
	}
}

func TestKittyPlaceholdersOutOfRange(t *testing.T) {
	if got := KittyPlaceholders(1, len(kittyDiacritics)+1, 2); got != "" {
		t.Error("expected empty result when cols exceed diacritic table")
	}
}
