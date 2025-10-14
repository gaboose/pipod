package podman

import (
	"encoding/json"
	"fmt"
	"io"
	"os/exec"

	"github.com/gaboose/pipod/internal/iio"
)

// Image is a pipod image handle.
type Image struct {
	name string
}

type imageInspect []struct {
	Config struct {
		Labels json.RawMessage `json:"Labels"`
	} `json:"Config"`
}

func (i *Image) Name() string {
	return i.name
}

func (i *Image) UnmarshalLabels(labels any) error {
	cmd := exec.Command("podman", "inspect", i.name, "--format", "json")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("inspect failed: %w", err)
	}

	var inspect imageInspect
	if err := json.Unmarshal(out, &inspect); err != nil {
		return fmt.Errorf("unmarshal failed: %w", err)
	}
	if len(inspect) == 0 {
		return fmt.Errorf("no inspect data returned for image %s", i.name)
	}

	return json.Unmarshal(inspect[0].Config.Labels, labels)
}

func (i *Image) TarOut() (io.ReadCloser, error) {
	ctx, closer := iio.ContextCloser()
	cmd := exec.CommandContext(ctx, "podman", "unshare", "bash", "-c", fmt.Sprintf("tar cC $(podman image mount %q) .", i.name))
	pr, pw := io.Pipe()
	cmd.Stdout = pw

	go func() {
		pr.CloseWithError(cmd.Run())
	}()

	return closer.WithReader(pr), nil
}
