package connman

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"text/template"

	_ "embed"

	"github.com/gaboose/pipod/internal/glibkeyfile"
	"github.com/spf13/afero"
	"gopkg.in/ini.v1"
)

const (
	CONNMAN_STORAGE_DIR       = "/var/lib/connman"
	CONNMAN_SETTINGS_FILE     = "/var/lib/connman/settings"
	CONNMAN_PROVISIONING_FILE = "/var/lib/connman/pipod.config"
)

var connmandPaths = []string{
	"/usr/sbin/connmand",
	"/sbin/connmand",
}

//go:embed conf.template
var confTemplate string

//go:embed settings
var settings []byte

type ConnMan struct {
	fs afero.Fs
}

func NewConnMan(afs afero.Fs) (*ConnMan, error) {
	for _, path := range connmandPaths {
		st, err := afs.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("failed to stat: %w", err)
		} else if st.IsDir() {
			return nil, fmt.Errorf("%s is a dir", path)
		}

		return &ConnMan{fs: afs}, nil
	}

	return nil, fmt.Errorf("connmand not found in %s: %w", strings.Join(connmandPaths, ", "), fs.ErrNotExist)
}

func (c *ConnMan) AddConnection(ssid string, password string) ([]string, error) {
	type Details struct {
		SSID     string
		Password string
	}

	added := []string{}
	if err := c.fs.MkdirAll(CONNMAN_STORAGE_DIR, 0700); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", CONNMAN_STORAGE_DIR, err)
	}

	t := template.Must(template.New("conf").Parse(confTemplate))

	// Contains the passphrase, keep it readable by root only.
	f, err := c.fs.OpenFile(CONNMAN_PROVISIONING_FILE, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to create file %s: %w", CONNMAN_PROVISIONING_FILE, err)
	}
	if err := t.Execute(f, Details{
		SSID:     hex.EncodeToString([]byte(ssid)),
		Password: glibkeyfile.EscapeValue(password),
	}); err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("failed to close %s: %w", CONNMAN_PROVISIONING_FILE, err)
	}
	added = append(added, CONNMAN_PROVISIONING_FILE)

	origSettings, err := afero.ReadFile(c.fs, CONNMAN_SETTINGS_FILE)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("failed to read file %s: %w", CONNMAN_SETTINGS_FILE, err)
	}

	newSettings, err := mergeKeyFiles(origSettings, settings)
	if err != nil {
		return nil, fmt.Errorf("failed to merge settings: %w", err)
	}

	if err := afero.WriteFile(c.fs, CONNMAN_SETTINGS_FILE, newSettings, 0600); err != nil {
		return nil, fmt.Errorf("failed to write file %s: %w", CONNMAN_SETTINGS_FILE, err)
	}
	added = append(added, CONNMAN_SETTINGS_FILE)

	return added, nil
}

func mergeKeyFiles(file1 []byte, file2 []byte) ([]byte, error) {
	cfg1, err := ini.Load(file1)
	if err != nil {
		return nil, fmt.Errorf("failed to load file1: %w", err)
	}

	cfg2, err := ini.Load(file2)
	if err != nil {
		return nil, fmt.Errorf("failed to load file2: %w", err)
	}

	for _, section2 := range cfg2.Sections() {
		section1, err := cfg1.GetSection(section2.Name())
		if err != nil {
			section1, _ = cfg1.NewSection(section2.Name())
		}

		for _, key2 := range section2.Keys() {
			_, _ = section1.NewKey(key2.Name(), key2.Value())
		}
	}

	buf := bytes.NewBuffer(nil)
	if _, err := cfg1.WriteTo(buf); err != nil {
		return nil, fmt.Errorf("failed to write to buf: %w", err)
	}

	return buf.Bytes(), nil
}
