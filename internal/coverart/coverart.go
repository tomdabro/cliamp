// Package coverart fetches album cover images and renders them as colored
// half-block text for display in the terminal UI.
//
// A cover is drawn with the upper-half-block rune "▀": each character cell
// carries two vertical pixels — the foreground color paints the top pixel and
// the background color paints the bottom pixel. This doubles vertical
// resolution and, unlike terminal graphics protocols (Kitty/Sixel), composes
// cleanly with the cell-based renderer.
//
// Rendering emits 24-bit ("truecolor") SGR sequences directly; callers should
// only display covers on truecolor terminals.
package coverart

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	// Register the image decoders cliamp's providers actually serve.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/bjarneo/cliamp/internal/appdir"
)

const (
	// maxImageBytes bounds a single cover download/decode.
	maxImageBytes = 8 << 20
	// cacheSubdir under the data dir holds downloaded cover bytes.
	cacheSubdir = "cover-cache"
	// cacheMaxBytes caps the on-disk cover cache; oldest files are evicted.
	cacheMaxBytes = 32 << 20
)

// httpGet is a bounded client for cover downloads. Distinct from
// httpclient.Streaming (which has no overall timeout) because a cover fetch
// must not hang the async load.
var httpGet = &http.Client{Timeout: 15 * time.Second}

// Image fetches and decodes the cover at srcURL, which may be an http(s) or
// file:// URL. Remote downloads are cached on disk keyed by URL so repeated
// plays of the same album don't re-fetch.
func Image(ctx context.Context, srcURL string) (image.Image, error) {
	data, err := fetchBytes(ctx, srcURL)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("coverart: decode %q: %w", srcURL, err)
	}
	return img, nil
}

// fetchBytes returns the raw image bytes for srcURL, reading file:// URLs from
// disk and caching http(s) downloads under the data dir.
func fetchBytes(ctx context.Context, srcURL string) ([]byte, error) {
	if srcURL == "" {
		return nil, fmt.Errorf("coverart: empty url")
	}
	if strings.HasPrefix(srcURL, "file://") {
		u, err := url.Parse(srcURL)
		if err != nil {
			return nil, fmt.Errorf("coverart: parse %q: %w", srcURL, err)
		}
		return os.ReadFile(u.Path)
	}

	cachePath, cacheable := coverCachePath(srcURL)
	if cacheable {
		if data, err := os.ReadFile(cachePath); err == nil {
			now := time.Now()
			_ = os.Chtimes(cachePath, now, now) // keep recently used files
			return data, nil
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srcURL, nil)
	if err != nil {
		return nil, fmt.Errorf("coverart: request %q: %w", srcURL, err)
	}
	resp, err := httpGet.Do(req)
	if err != nil {
		return nil, fmt.Errorf("coverart: fetch %q: %w", srcURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("coverart: fetch %q: http %s", srcURL, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes))
	if err != nil {
		return nil, fmt.Errorf("coverart: read %q: %w", srcURL, err)
	}
	if cacheable {
		writeCache(cachePath, data)
	}
	return data, nil
}

// coverCachePath returns the on-disk cache path for a source URL and whether a
// cache location could be resolved.
func coverCachePath(srcURL string) (string, bool) {
	dir, err := appdir.DataDir()
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256([]byte(srcURL))
	return filepath.Join(dir, cacheSubdir, hex.EncodeToString(sum[:])), true
}

// writeCache stores data at path (creating the cache dir) and evicts the oldest
// files when the cache exceeds cacheMaxBytes. Errors are ignored: the cache is
// an optimization, not a correctness requirement.
func writeCache(path string, data []byte) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return
	}
	evictCache(dir, cacheMaxBytes, path)
}

// evictCache removes the least-recently-modified files until the directory is
// within maxBytes, never removing keepPath.
func evictCache(dir string, maxBytes int64, keepPath string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type cached struct {
		path    string
		size    int64
		modTime time.Time
	}
	files := make([]cached, 0, len(entries))
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, cached{path: filepath.Join(dir, e.Name()), size: info.Size(), modTime: info.ModTime()})
		total += info.Size()
	}
	if total <= maxBytes {
		return
	}
	// Oldest first.
	for i := 1; i < len(files); i++ {
		for j := i; j > 0 && files[j-1].modTime.After(files[j].modTime); j-- {
			files[j-1], files[j] = files[j], files[j-1]
		}
	}
	for _, f := range files {
		if total <= maxBytes {
			break
		}
		if f.path == keepPath {
			continue
		}
		if err := os.Remove(f.path); err == nil {
			total -= f.size
		}
	}
}

// HalfBlocks renders img as cols×rows character cells using the upper-half-block
// rune: the foreground paints the top pixel and the background the bottom, so a
// cell carries two vertical pixels. Each output pixel is the area-average of the
// source pixels that map to it (box downscale), which is far smoother than
// point sampling at these tiny sizes. The result is rows lines separated by
// "\n"; each line is cols cells wide and ends with an SGR reset. Returns "" for
// a non-positive size or empty image.
func HalfBlocks(img image.Image, cols, rows int) string {
	if img == nil || cols <= 0 || rows <= 0 {
		return ""
	}
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	if srcW <= 0 || srcH <= 0 {
		return ""
	}

	pxH := rows * 2
	// avg returns the mean 8-bit RGB over the source rectangle mapped to output
	// pixel (x,y) in the cols×pxH grid.
	avg := func(x, y int) (uint8, uint8, uint8) {
		x0 := b.Min.X + x*srcW/cols
		x1 := b.Min.X + (x+1)*srcW/cols
		y0 := b.Min.Y + y*srcH/pxH
		y1 := b.Min.Y + (y+1)*srcH/pxH
		if x1 <= x0 {
			x1 = x0 + 1
		}
		if y1 <= y0 {
			y1 = y0 + 1
		}
		var rs, gs, bs, n uint64
		for yy := y0; yy < y1; yy++ {
			for xx := x0; xx < x1; xx++ {
				r, g, bl, _ := img.At(xx, yy).RGBA()
				rs += uint64(r >> 8)
				gs += uint64(g >> 8)
				bs += uint64(bl >> 8)
				n++
			}
		}
		if n == 0 {
			return 0, 0, 0
		}
		return uint8(rs / n), uint8(gs / n), uint8(bs / n)
	}

	var out strings.Builder
	out.Grow(rows * (cols*24 + 8))
	for row := range rows {
		for col := range cols {
			tr, tg, tb := avg(col, row*2)
			br, bg, bb := avg(col, row*2+1)
			fmt.Fprintf(&out, "\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm▀", tr, tg, tb, br, bg, bb)
		}
		out.WriteString("\x1b[0m")
		if row < rows-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}
