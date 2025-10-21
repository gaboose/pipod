package main

import (
	"fmt"

	"github.com/gaboose/pipod/internal/podman"
)

type PlatformSpec struct {
	Labels podman.PipodLabels `toml:"labels"`
}

func (ps PlatformSpec) validate() error {
	return ps.Labels.Validate()
}

type Spec struct {
	Labels   map[string]string       `toml:"labels"`
	Platform map[string]PlatformSpec `toml:"platform"`
}

func (s *Spec) validate() error {
	if len(s.Platform) == 0 {
		return fmt.Errorf(`"platform" is empty`)
	}

	for name, platform := range s.Platform {
		if err := platform.validate(); err != nil {
			return fmt.Errorf(`platform.%s.labels: %w`, name, err)
		}
	}

	return nil
}
