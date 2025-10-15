package podman

import (
	"encoding/json"
	"fmt"
	"io"
	"os/exec"

	"github.com/gaboose/pipod/internal/iio"
	"github.com/pelletier/go-toml/v2"
)

// Image is a pipod image handle.
type Image struct {
	Name string
}

type imageInspect []struct {
	Labels json.RawMessage `json:"Labels"`
}

func (i *Image) UnmarshalLabelsJson(labels any) error {
	cmd := exec.Command("podman", "inspect", i.Name, "--format", "json")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("inspect failed: %w", err)
	}

	var inspect imageInspect
	if err := json.Unmarshal(out, &inspect); err != nil {
		return fmt.Errorf("unmarshal failed: %w", err)
	}
	if len(inspect) == 0 {
		return fmt.Errorf("no inspect data returned for image %s", i.Name)
	}

	return json.Unmarshal(inspect[0].Labels, labels)
}

func (i *Image) UnmarshalLabelsToml(labels any) error {
	jsonMap := map[string]string{}
	if err := i.UnmarshalLabelsJson(&jsonMap); err != nil {
		return fmt.Errorf("failed to json unmarshal labels: %w", err)
	}

	bts, err := toml.Marshal(jsonMap)
	if err != nil {
		return fmt.Errorf("failed to toml marshal labels: %w", err)
	}

	return toml.Unmarshal(bts, labels)
}

func (i Image) TarOut() (io.ReadCloser, error) {
	ctx, closer := iio.ContextCloser()
	cmd := exec.CommandContext(ctx, "podman", "unshare", "bash", "-c", fmt.Sprintf("tar cC $(podman image mount %q) .", i.Name))
	pr, pw := io.Pipe()
	cmd.Stdout = pw

	go func() {
		pr.CloseWithError(cmd.Run())
	}()

	return closer.WithReader(pr), nil
}
