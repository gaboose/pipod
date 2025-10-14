package podman

import (
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type importOpts struct {
	os      string
	arch    string
	variant string
	labels  map[string]string
	tags    []string
	errs    []error
}

type importOpt func(*importOpts)

func (o importOpt) applyImportOpt(opts *importOpts) { o(opts) }

type ImportOption interface {
	applyImportOpt(*importOpts)
}

func WithOS(os string) ImportOption {
	return importOpt(func(opts *importOpts) {
		opts.os = os
	})
}

func WithArch(arch string) ImportOption {
	return importOpt(func(opts *importOpts) {
		opts.arch = arch
	})
}

func WithVariant(variant string) ImportOption {
	return importOpt(func(opts *importOpts) {
		opts.variant = variant
	})
}

func WithPlatform(platform string) ImportOption {
	return importOpt(func(opts *importOpts) {
		parts := strings.Split(platform, "/")
		if len(parts) < 2 || len(parts) > 3 {
			opts.errs = append(opts.errs, fmt.Errorf("failed to parse platform %s", platform))
			return
		}

		opts.os = parts[0]
		opts.arch = parts[1]
		if len(parts) > 2 {
			opts.variant = parts[2]
		}
	})
}

func WithTags(tags ...string) Option {
	return Option{
		importOpt: func(opts *importOpts) {
			opts.tags = append(opts.tags, tags...)
		},
		manifestCreateOpt: func(opts *manifestCreateOpts) {
			opts.tags = append(opts.tags, tags...)
		},
	}
}

func WithLabels(labels map[string]string) ImportOption {
	return importOpt(func(opts *importOpts) {
		if len(labels) > 0 && opts.labels == nil {
			opts.labels = make(map[string]string, len(labels))
		}
		for k, v := range labels {
			opts.labels[k] = v
		}
	})
}

func WithLabelsToml(labels any) ImportOption {
	return importOpt(func(opts *importOpts) {
		bts, err := toml.Marshal(labels)
		if err != nil {
			opts.errs = append(opts.errs, fmt.Errorf("failed to marshal labels: %w", err))
			return
		}

		labelsMap := map[string]string{}
		if err := toml.Unmarshal(bts, &labelsMap); err != nil {
			opts.errs = append(opts.errs, fmt.Errorf("failed to unmarshal labels: %w", err))
			return
		}

		WithLabels(labelsMap).applyImportOpt(opts)
	})
}

func Import(name string, reader io.ReadCloser, opts ...ImportOption) error {
	defer reader.Close()

	var oo = importOpts{}
	for _, o := range opts {
		o.applyImportOpt(&oo)
	}
	if len(oo.errs) > 0 {
		return fmt.Errorf("failed to set options: %w", oo.errs[0])
	}

	podmanArgs := []string{"import", "-", name}

	if oo.os != "" {
		podmanArgs = append(podmanArgs, "--os", oo.os)
	}

	if oo.arch != "" {
		podmanArgs = append(podmanArgs, "--arch", oo.arch)
	}

	if oo.variant != "" {
		podmanArgs = append(podmanArgs, "--variant", oo.variant)
	}

	for k, v := range oo.labels {
		podmanArgs = append(podmanArgs, "--change", fmt.Sprintf("LABEL %q=%q", k, v))
	}

	podmanCmd := exec.Command("podman", podmanArgs...)
	podmanCmd.Stdin = reader
	podmanCmd.Stdout = stdout
	podmanCmd.Stderr = stderr

	if err := podmanCmd.Run(); err != nil {
		return fmt.Errorf("failed to run podman import: %w", err)
	}

	if len(oo.tags) > 0 {
		if err := Tag(name, oo.tags...); err != nil {
			return fmt.Errorf("failed to tag image: %w", err)
		}
	}

	return nil
}
