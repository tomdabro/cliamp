package coverart

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// kittyPlaceholderRune is the Unicode placeholder used to bind terminal cells
// to a Kitty graphics virtual placement.
const kittyPlaceholderRune = '\U0010EEEE'

// kittyChunk is the maximum base64 payload per Kitty graphics APC chunk (the
// protocol caps transmit chunks at 4096 base64 bytes).
const kittyChunk = 4096

// MaxKittyID is the largest image id that fits in a 24-bit foreground color,
// which is how Unicode placeholders reference their image (no extra diacritic
// needed for the high byte).
const MaxKittyID = 0xFFFFFF

// KittySupported reports whether the current terminal, per its environment,
// speaks the Kitty graphics protocol with Unicode placeholders. Recognizes
// Kitty, Ghostty, and WezTerm.
func KittySupported() bool {
	if os.Getenv("KITTY_WINDOW_ID") != "" || os.Getenv("GHOSTTY_RESOURCES_DIR") != "" {
		return true
	}
	switch strings.ToLower(os.Getenv("TERM_PROGRAM")) {
	case "ghostty", "wezterm":
		return true
	}
	term := os.Getenv("TERM")
	return strings.Contains(term, "kitty") || strings.Contains(term, "ghostty")
}

// KittyTransmit returns the escape sequences that load img into the terminal
// under image id, sized to a cols×rows cell box via a Unicode-placeholder
// virtual placement. When deletePrev is non-zero, the prior image is deleted
// first to free terminal memory. The result is meant to be written to the tty
// out-of-band (e.g. via tea.Raw), not through the cell renderer.
func KittyTransmit(id uint32, img image.Image, cols, rows int, deletePrev uint32) (string, error) {
	if img == nil || cols <= 0 || rows <= 0 {
		return "", fmt.Errorf("coverart: kitty transmit: empty image or size")
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		return "", fmt.Errorf("coverart: png encode: %w", err)
	}
	data := base64.StdEncoding.EncodeToString(pngBuf.Bytes())

	var b strings.Builder
	if deletePrev != 0 {
		// d=I deletes the image and frees its data.
		b.WriteString(ansi.KittyGraphics(nil, "a=d", "d=I", fmt.Sprintf("i=%d", deletePrev)))
	}
	for first := true; len(data) > 0; first = false {
		n := min(kittyChunk, len(data))
		piece := data[:n]
		data = data[n:]
		more := "m=0"
		if len(data) > 0 {
			more = "m=1"
		}
		var opts []string
		if first {
			// a=t transmit, f=100 PNG, q=2 suppress terminal responses.
			opts = []string{"a=t", "f=100", fmt.Sprintf("i=%d", id), "q=2", more}
		} else {
			opts = []string{more}
		}
		b.WriteString(ansi.KittyGraphics([]byte(piece), opts...))
	}
	// a=p virtual placement (U=1) sized to the cell box; Unicode placeholders
	// in the rendered frame reference it by id.
	b.WriteString(ansi.KittyGraphics(nil, "a=p", fmt.Sprintf("i=%d", id), "U=1",
		fmt.Sprintf("c=%d", cols), fmt.Sprintf("r=%d", rows), "q=2"))
	return b.String(), nil
}

// KittyPlaceholders renders the cols×rows grid of Unicode placeholder cells that
// display image id (previously transmitted with a matching virtual placement).
// Each cell is U+10EEEE plus row/column diacritics, with the image id encoded in
// the 24-bit foreground color. Returns "" when the size exceeds the diacritic
// table so the caller can fall back to half-blocks.
func KittyPlaceholders(id uint32, cols, rows int) string {
	if cols <= 0 || rows <= 0 || cols > len(kittyDiacritics) || rows > len(kittyDiacritics) {
		return ""
	}
	r, g, bl := byte(id>>16), byte(id>>8), byte(id)
	var b strings.Builder
	b.Grow(rows * (cols*8 + 16))
	for row := range rows {
		fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm", r, g, bl)
		for col := range cols {
			b.WriteRune(kittyPlaceholderRune)
			b.WriteRune(kittyDiacritics[row])
			b.WriteRune(kittyDiacritics[col])
		}
		b.WriteString("\x1b[0m")
		if row < rows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
