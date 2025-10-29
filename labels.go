package main

import "fmt"

type PipodLabels struct {
	SourceURL              string `toml:"com.github.gaboose.pipod.source.url"`
	SourceSHA256           string `toml:"com.github.gaboose.pipod.source.sha256,omitempty"`
	SourcePartitionsImport string `toml:"com.github.gaboose.pipod.source.partitions.import,omitempty"`
}

func (pdl *PipodLabels) validate() error {
	if pdl.SourceURL == "" {
		return fmt.Errorf(`com.github.gaboose.pipod.source.url not found`)
	}

	return nil
}

func (pdl *PipodLabels) GetSourcePartitionsImport() string {
	return withDefault(pdl.SourcePartitionsImport, "sda2")
}

func withDefault(target string, def string) string {
	if target != "" {
		return target
	} else {
		return def
	}
}
