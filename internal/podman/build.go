package podman

import (
	"io"
	"os/exec"

	"github.com/gaboose/pipod/internal/iio"
)

// Build runs podman build.
func Build(platform string) (*Image, error) {
	lastLineWriter, lastLineBuf := iio.LastLine()

	cmd := exec.Command("podman", "build", ".", "--platform", platform)
	cmd.Stdout = io.MultiWriter(stdout, lastLineWriter)
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		return nil, err
	}

	return &Image{Name: lastLineBuf.String()}, nil
}
