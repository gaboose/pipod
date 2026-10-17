package wifi

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	aferoguestfs "github.com/gaboose/afero-guestfs"
	"github.com/gaboose/pipod/internal/wifi/connman"
	"github.com/gaboose/pipod/internal/wifi/networkmanager"
	"github.com/spf13/afero"
)

type SetupResult struct {
	Kind       string
	AddedPaths []string
}

func (r SetupResult) String() string {
	var s strings.Builder

	if r.Kind != "" {
		fmt.Fprintf(&s, "%s detected\n", r.Kind)
	}

	for _, path := range r.AddedPaths {
		fmt.Fprintf(&s, "added %s\n", path)
	}

	return s.String()
}

type Config struct {
	SSID     string `toml:"ssid"`
	Password string `toml:"password"`
}

func (c *Config) Validate() error {
	if c.SSID == "" {
		return fmt.Errorf("ssid must be set in config")
	}

	if c.Password == "" {
		return fmt.Errorf("password must be set in config")
	}

	allAscii := true
	for _, r := range c.Password {
		if r > 0x7E || r < 0x20 {
			allAscii = false
			break
		}
	}

	if len(c.Password) < 8 || len(c.Password) > 63 || !allAscii {
		return fmt.Errorf("password must be 8-63 ascii characters")
	}

	return nil
}

func (c *Config) Setup(disk string, partition string) (SetupResult, error) {
	afs, err := aferoguestfs.OpenPartitionFs(disk, partition)
	if err != nil {
		return SetupResult{}, fmt.Errorf("failed to open partition: %w", err)
	}
	defer func() {
		if cerr := afs.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close partition: %w", cerr)
		}
	}()

	type connectionAdder interface {
		AddConnection(ssid string, password string) ([]string, error)
	}

	managers := []struct {
		kind string
		new  func(afero.Fs) (connectionAdder, error)
	}{{
		kind: "NetworkManager",
		new:  func(fs afero.Fs) (connectionAdder, error) { return networkmanager.NewNetworkManager(fs) },
	}, {
		kind: "ConnMan",
		new:  func(fs afero.Fs) (connectionAdder, error) { return connman.NewConnMan(fs) },
	}}

	var errs []error
	for _, m := range managers {
		mgr, err := m.new(afs)
		if errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("%s: %w", m.kind, err))
			continue
		} else if err != nil {
			return SetupResult{}, fmt.Errorf("failed to create %s: %w", m.kind, err)
		}

		res := SetupResult{Kind: m.kind}
		res.AddedPaths, err = mgr.AddConnection(c.SSID, c.Password)
		if err != nil {
			return SetupResult{}, fmt.Errorf("failed to add %s connection: %w", m.kind, err)
		}

		return res, nil
	}

	return SetupResult{}, fmt.Errorf("no supported network manager found: %w", errors.Join(errs...))
}
