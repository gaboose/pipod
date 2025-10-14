package podman

import (
	"fmt"
	"os/exec"
)

type manifestCreateOpts struct {
	tags []string
}

type manifestCreateOpt func(*manifestCreateOpts)

func (o manifestCreateOpt) applyManifestCreateOpt(opts *manifestCreateOpts) { o(opts) }

type ManifestCreateOption interface {
	applyManifestCreateOpt(*manifestCreateOpts)
}

func CreateManifest(name string, images []string, opts ...ManifestCreateOption) error {
	var oo manifestCreateOpts
	for _, o := range opts {
		o.applyManifestCreateOpt(&oo)
	}

	args := []string{"manifest", "create", name}
	args = append(args, images...)

	cmd := exec.Command("podman", args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("manifest create failed: %w", err)
	}

	if len(oo.tags) > 0 {
		if err := Tag(name, oo.tags...); err != nil {
			return fmt.Errorf("failed to tag image: %w", err)
		}
	}

	return nil
}
