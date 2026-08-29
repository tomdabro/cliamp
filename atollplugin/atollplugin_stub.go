//go:build !darwin

package atollplugin

import (
	"time"

	"github.com/bjarneo/cliamp/internal/playback"
)

// Service is a no-op on non-macOS platforms: AtollPluginManager only exists
// on macOS.
type Service struct{}

func New() (*Service, error) { return nil, nil }

func (s *Service) Update(playback.State) {}
func (s *Service) Seeked(time.Duration)  {}
func (s *Service) Close() error          { return nil }
