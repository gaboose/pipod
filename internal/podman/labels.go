package podman

import "fmt"

type PipodLabels struct {
	SourceURL              string `toml:"com.github.gaboose.pipod.source.url"`
	SourceSHA256           string `toml:"com.github.gaboose.pipod.source.sha256"`
	SourcePartitionsImport string `toml:"com.github.gaboose.pipod.source.partitions.import"`
	TargetFilename         string `toml:"com.github.gaboose.pipod.target.filename,omitempty"`
}

func (pdl PipodLabels) Validate() error {
	if pdl.SourceURL == "" {
		return fmt.Errorf(`com.github.gaboose.pipod.disk.source not found`)
	}
	if pdl.SourceSHA256 == "" {
		return fmt.Errorf(`com.github.gaboose.pipod.disk.sha256 not found`)
	}
	if pdl.SourcePartitionsImport == "" {
		return fmt.Errorf(`com.github.gaboose.pipod.disk.partition not found`)
	}

	return nil
}
