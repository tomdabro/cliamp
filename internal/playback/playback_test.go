package playback

import (
	"testing"
	"time"
)

func TestStatusConstants(t *testing.T) {
	tests := []struct {
		s    Status
		want string
	}{
		{StatusStopped, "Stopped"},
		{StatusPlaying, "Playing"},
		{StatusPaused, "Paused"},
	}
	for _, tt := range tests {
		if string(tt.s) != tt.want {
			t.Errorf("Status = %q, want %q", string(tt.s), tt.want)
		}
	}
}

func TestSeekMsgOffset(t *testing.T) {
	m := SeekMsg{Offset: 5 * time.Second}
	if m.Offset != 5*time.Second {
		t.Errorf("Offset = %v, want 5s", m.Offset)
	}
}

func TestSetPositionMsgPosition(t *testing.T) {
	m := SetPositionMsg{Position: 42 * time.Second}
	if m.Position != 42*time.Second {
		t.Errorf("Position = %v, want 42s", m.Position)
	}
}

func TestSetVolumeMsgDB(t *testing.T) {
	m := SetVolumeMsg{VolumeDB: -6.0}
	if m.VolumeDB != -6.0 {
		t.Errorf("VolumeDB = %f, want -6.0", m.VolumeDB)
	}
}

func TestStateFields(t *testing.T) {
	s := State{
		Status: StatusPlaying,
		Track: Track{
			Title:       "Song",
			Artist:      "Artist",
			Album:       "Album",
			Genre:       "Rock",
			TrackNumber: 3,
			URL:         "file:///song.mp3",
			ArtURL:      "file:///cover.jpg",
			Duration:    3 * time.Minute,
		},
		VolumeDB: -3.0,
		Position: time.Minute,
		Seekable: true,
	}
	if s.Status != StatusPlaying {
		t.Errorf("Status = %q, want Playing", s.Status)
	}
	if s.Track.Title != "Song" {
		t.Errorf("Track.Title = %q, want Song", s.Track.Title)
	}
	if s.Track.Duration != 3*time.Minute {
		t.Errorf("Track.Duration = %v, want 3m", s.Track.Duration)
	}
	if !s.Seekable {
		t.Error("Seekable = false, want true")
	}
}

// fakeNotifier confirms the Notifier interface is satisfiable.
type fakeNotifier struct {
	updates []State
	seeks   []time.Duration
}

func (f *fakeNotifier) Update(s State)         { f.updates = append(f.updates, s) }
func (f *fakeNotifier) Seeked(d time.Duration) { f.seeks = append(f.seeks, d) }

func TestNotifierInterface(t *testing.T) {
	var n Notifier = &fakeNotifier{}
	n.Update(State{Status: StatusPlaying})
	n.Seeked(time.Second)

	f := n.(*fakeNotifier)
	if len(f.updates) != 1 || f.updates[0].Status != StatusPlaying {
		t.Errorf("updates = %+v, want one Playing", f.updates)
	}
	if len(f.seeks) != 1 || f.seeks[0] != time.Second {
		t.Errorf("seeks = %v, want [1s]", f.seeks)
	}
}

func TestMultiFansOutToEveryNotifier(t *testing.T) {
	a, b := &fakeNotifier{}, &fakeNotifier{}
	n := Multi(a, b)

	n.Update(State{Status: StatusPlaying})
	n.Seeked(2 * time.Second)

	for i, f := range []*fakeNotifier{a, b} {
		if len(f.updates) != 1 || f.updates[0].Status != StatusPlaying {
			t.Errorf("notifier %d updates = %+v, want one Playing", i, f.updates)
		}
		if len(f.seeks) != 1 || f.seeks[0] != 2*time.Second {
			t.Errorf("notifier %d seeks = %v, want [2s]", i, f.seeks)
		}
	}
}

func TestMultiSkipsNilNotifiers(t *testing.T) {
	a := &fakeNotifier{}
	n := Multi(a, nil)

	n.Update(State{Status: StatusPaused})

	if len(a.updates) != 1 {
		t.Errorf("updates = %+v, want one entry", a.updates)
	}
}

func TestMultiWithNoNotifiersReturnsNil(t *testing.T) {
	if n := Multi(); n != nil {
		t.Errorf("Multi() = %v, want nil", n)
	}
	if n := Multi(nil, nil); n != nil {
		t.Errorf("Multi(nil, nil) = %v, want nil", n)
	}
}

func TestMultiWithOneNotifierReturnsItUnwrapped(t *testing.T) {
	a := &fakeNotifier{}
	n := Multi(a)
	if n != Notifier(a) {
		t.Errorf("Multi(a) did not return a unwrapped")
	}
}
