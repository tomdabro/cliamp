package model

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestCoverShownInFullTier(t *testing.T) {
	base := newLayoutTestModel(100, 40)
	baseFixed := base.layout.fixedRows

	m := newLayoutTestModel(100, 40)
	m.SetAlbumArtHeight(7)
	m.recomputeLayout()

	if !m.cover.shown {
		t.Fatal("cover.shown = false, want true in full tier with art enabled")
	}
	if got, want := m.layout.fixedRows, baseFixed+(7-coverHeaderRows); got != want {
		t.Fatalf("fixedRows = %d, want %d (baseline %d + %d extra rows)", got, want, baseFixed, 7-coverHeaderRows)
	}
	m.cover.rendered = strings.TrimSuffix(strings.Repeat("COVER-SENTINEL\n", 7), "\n")
	header := m.renderNowPlayingHeader()
	if !strings.Contains(header, "COVER-SENTINEL") {
		t.Fatalf("header missing cover art: %q", header)
	}
	if h := lipgloss.Height(header); h != 7 {
		t.Fatalf("header height = %d, want 7 (cover rows)", h)
	}
	if got := lipgloss.Height(m.View().Content); got > 40 {
		t.Fatalf("view height = %d, want <= 40 (must fit terminal)", got)
	}
}

func TestCoverPlaceholderWhenUnloaded(t *testing.T) {
	m := newLayoutTestModel(100, 40)
	m.SetAlbumArtHeight(6)
	m.recomputeLayout()
	m.cover.rendered = "" // not yet loaded
	col := m.renderCoverColumn()
	if lipgloss.Height(col) != 6 {
		t.Fatalf("placeholder height = %d, want 6", lipgloss.Height(col))
	}
	if w := lipgloss.Width(col); w != m.cover.cols {
		t.Fatalf("placeholder width = %d, want %d", w, m.cover.cols)
	}
}

func TestCoverHiddenWhenDisabled(t *testing.T) {
	m := newLayoutTestModel(100, 40)
	m.SetAlbumArtHeight(0)
	m.recomputeLayout()
	if m.cover.shown {
		t.Fatal("cover.shown = true, want false when disabled")
	}
	if h := lipgloss.Height(m.renderNowPlayingHeader()); h != 3 {
		t.Fatalf("header height = %d, want 3 (plain stack)", h)
	}
}

func TestCoverHiddenInCompactTier(t *testing.T) {
	m := newLayoutTestModel(60, 18) // compact tier: too narrow/short for the cover column
	m.SetAlbumArtHeight(7)
	m.recomputeLayout()
	if m.layout.tier == layoutFull {
		t.Fatalf("tier = full, want a smaller tier for this test")
	}
	if m.cover.shown {
		t.Fatal("cover.shown = true in non-full tier, want false")
	}
}

func TestSetAlbumArtProtocol(t *testing.T) {
	var m Model
	m.SetAlbumArtProtocol("kitty")
	if !m.cover.kitty {
		t.Error(`protocol "kitty" should enable kitty rendering`)
	}
	m.SetAlbumArtProtocol("blocks")
	if m.cover.kitty {
		t.Error(`protocol "blocks" should disable kitty rendering`)
	}
	t.Setenv("KITTY_WINDOW_ID", "1")
	m.SetAlbumArtProtocol("auto")
	if !m.cover.kitty {
		t.Error(`protocol "auto" should detect kitty from KITTY_WINDOW_ID`)
	}
}

func TestNextKittyIDWraps(t *testing.T) {
	if got := nextKittyID(0); got != 1 {
		t.Errorf("nextKittyID(0) = %d, want 1", got)
	}
	if got := nextKittyID(0xFFFFFF); got != 1 {
		t.Errorf("nextKittyID(max) = %d, want wrap to 1", got)
	}
}

func TestCoverLoadedEmitsRawTransmit(t *testing.T) {
	m := newLayoutTestModel(100, 40)
	m.SetAlbumArtHeight(7)
	m.cover.url = "spotify://cover"
	m.requests.cover = 3

	updated, cmd := m.Update(coverLoadedMsg{
		url:      "spotify://cover",
		rendered: "PLACEHOLDERS",
		transmit: "\x1b_Gtransmit\x1b\\",
		gen:      3,
	})
	m = updated.(Model)
	if m.cover.rendered != "PLACEHOLDERS" {
		t.Fatalf("rendered = %q, want PLACEHOLDERS", m.cover.rendered)
	}
	if cmd == nil {
		t.Fatal("expected a command to transmit the image, got nil")
	}
	raw, ok := cmd().(tea.RawMsg)
	if !ok {
		t.Fatalf("command message = %T, want tea.RawMsg", cmd())
	}
	if raw.Msg != "\x1b_Gtransmit\x1b\\" {
		t.Fatalf("raw transmit = %q, want the kitty sequence", raw.Msg)
	}
}

func TestCoverLoadedStaleGenIgnored(t *testing.T) {
	m := newLayoutTestModel(100, 40)
	m.SetAlbumArtHeight(7)
	m.cover.url = "u"
	m.requests.cover = 5
	updated, cmd := m.Update(coverLoadedMsg{url: "u", rendered: "X", gen: 4})
	m = updated.(Model)
	if m.cover.rendered == "X" || cmd != nil {
		t.Fatal("stale cover load (wrong gen) must be ignored")
	}
}
