package playback

import "time"

type (
	PlayPauseMsg   struct{}
	PlayMsg        struct{}
	PauseMsg       struct{}
	NextMsg        struct{}
	PrevMsg        struct{}
	StopMsg        struct{}
	QuitMsg        struct{}
	SeekMsg        struct{ Offset time.Duration }
	SetPositionMsg struct {
		Position time.Duration
	}
	SetVolumeMsg struct{ VolumeDB float64 }
)

type Status string

const (
	StatusStopped Status = "Stopped"
	StatusPlaying Status = "Playing"
	StatusPaused  Status = "Paused"
)

type Track struct {
	Title       string
	Artist      string
	Album       string
	Genre       string
	TrackNumber int
	URL         string
	ArtURL      string
	Duration    time.Duration
}

type State struct {
	Status   Status
	Track    Track
	VolumeDB float64
	Position time.Duration
	Seekable bool
}

type Notifier interface {
	Update(State)
	Seeked(time.Duration)
}

// Multi fans Update/Seeked out to every non-nil notifier. Lets a caller
// attach more than one Notifier (e.g. mediactl for MPRIS/NowPlaying and
// atollplugin for Atoll) to the single playback.Notifier field on Model
// and daemon.
func Multi(notifiers ...Notifier) Notifier {
	kept := make([]Notifier, 0, len(notifiers))
	for _, n := range notifiers {
		if n != nil {
			kept = append(kept, n)
		}
	}
	switch len(kept) {
	case 0:
		return nil
	case 1:
		return kept[0]
	default:
		return multiNotifier(kept)
	}
}

type multiNotifier []Notifier

func (m multiNotifier) Update(state State) {
	for _, n := range m {
		n.Update(state)
	}
}

func (m multiNotifier) Seeked(position time.Duration) {
	for _, n := range m {
		n.Seeked(position)
	}
}
