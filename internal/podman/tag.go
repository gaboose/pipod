package podman

import (
	"fmt"
	"os/exec"
)

func Tag(image string, tags ...string) error {
	tagCmd := exec.Command("podman", append([]string{"image", "tag", image}, tags...)...)
	tagCmd.Stdout = stdout
	tagCmd.Stderr = stderr

	if err := tagCmd.Run(); err != nil {
		return fmt.Errorf("failed to start podman tag: %w", err)
	}

	return nil
}
