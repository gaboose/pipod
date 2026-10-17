package diskconfig

import (
	"fmt"
	"strings"

	aferoguestfs "github.com/gaboose/afero-guestfs"
	"github.com/gaboose/pipod/internal/openssl"
	"github.com/gaboose/pipod/internal/wifi"
	"github.com/spf13/afero"
)

type Config struct {
	Wifi *wifi.Config `toml:"wifi"`
	User *User        `toml:"user"`
	SSH  *SSH         `toml:"ssh"`
}

func (c *Config) Validate() error {
	if c.Wifi != nil {
		if err := c.Wifi.Validate(); err != nil {
			return fmt.Errorf("wifi: %w", err)
		}
	}

	if c.User != nil {
		if err := c.User.Validate(); err != nil {
			return fmt.Errorf("user: %w", err)
		}
	}

	if c.SSH != nil {
		if err := c.SSH.Validate(); err != nil {
			return fmt.Errorf("ssh: %w", err)
		}
	}

	return nil
}

type User struct {
	Username string `toml:"username"`
	Password string `toml:"password"`
}

type UserSetupResult struct {
	AddedPaths []string
}

func (r UserSetupResult) String() string {
	var s strings.Builder

	for _, path := range r.AddedPaths {
		fmt.Fprintf(&s, "added %s\n", path)
	}

	return s.String()
}

func (u *User) Validate() error {
	if u.Username == "" {
		return fmt.Errorf("username must be set in config")
	}

	if u.Password == "" {
		return fmt.Errorf("password must be set in config")
	}

	return nil
}

func (u *User) Setup(disk string, partition string) (UserSetupResult, error) {
	afs, err := aferoguestfs.OpenPartitionFs(disk, partition)
	if err != nil {
		return UserSetupResult{}, fmt.Errorf("failed to open partition: %w", err)
	}
	defer afs.Close()

	passwordHash, err := openssl.Passwd(u.Password)
	if err != nil {
		return UserSetupResult{}, fmt.Errorf("failed to hash password: %w", err)
	}

	if err = afero.WriteFile(afs, "/userconf.txt", fmt.Appendf(nil, "%s:%s", u.Username, passwordHash), 0644); err != nil {
		return UserSetupResult{}, fmt.Errorf("failed to write /userconf.txt: %w", err)
	}

	return UserSetupResult{AddedPaths: []string{"/userconf.txt"}}, nil
}

type SSH struct {
	Enabled bool `toml:"enabled"`
}

type SSHSetupResult struct {
	AddedPaths []string
}

func (r SSHSetupResult) String() string {
	var s strings.Builder

	for _, path := range r.AddedPaths {
		fmt.Fprintf(&s, "added %s\n", path)
	}

	return s.String()
}

func (s *SSH) Validate() error {
	return nil
}

func (s *SSH) Setup(disk string, partition string) (SSHSetupResult, error) {
	if !s.Enabled {
		return SSHSetupResult{}, nil
	}

	afs, err := aferoguestfs.OpenPartitionFs(disk, partition)
	if err != nil {
		return SSHSetupResult{}, fmt.Errorf("failed to open partition: %w", err)
	}
	defer afs.Close()

	if err = afero.WriteFile(afs, "/ssh.txt", nil, 0644); err != nil {
		return SSHSetupResult{}, fmt.Errorf("failed to write /ssh.txt: %w", err)
	}

	return SSHSetupResult{AddedPaths: []string{"/ssh.txt"}}, nil
}
