package wifi

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/google/uuid"
	"github.com/spf13/afero"
)

const (
	NETWORK_MANAGER_DIR               = "/etc/NetworkManager"
	NETWORK_MANAGER_CONF_WIFI_ON_FILE = "/etc/NetworkManager/conf.d/wifi-on.conf"
	NETWORK_MANAGER_STATE_FILE        = "/var/lib/NetworkManager/NetworkManager.state"
)

//go:embed nmconnection.template
var nmconnectionTemplate string

//go:embed wifi-on.conf
var networkManagerWifiOnConfContents []byte

//go:embed NetworkManager.state
var networkManagerStateContents []byte

type NetworkManager struct {
	fs afero.Fs
}

func NewNetworkManager(fs afero.Fs) (*NetworkManager, error) {
	st, err := fs.Stat(NETWORK_MANAGER_DIR)
	if err != nil {
		return nil, fmt.Errorf("failed to stat: %w", err)
	} else if !st.IsDir() {
		return nil, fmt.Errorf("%s is not a dir", NETWORK_MANAGER_DIR)
	}

	return &NetworkManager{
		fs: fs,
	}, nil
}

func (nm *NetworkManager) AddConnection(ssid string, password string) ([]string, error) {
	type Details struct {
		SSID     string
		Password string
	}

	t := template.Must(template.New("nmconnection").Funcs(template.FuncMap{
		"randomUUID": func() (string, error) {
			u, err := uuid.NewRandom()
			return u.String(), err
		},
	}).Parse(nmconnectionTemplate))

	added := []string{}
	connPath := filepath.Join(NETWORK_MANAGER_DIR, "system-connections", fmt.Sprintf("%s.nmconnection", ssid))

	if err := os.MkdirAll(filepath.Dir(connPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to MkdirAll: %w", err)
	}

	// Must be unreadable by other users or network manager refuses to use the profile
	f, err := nm.fs.OpenFile(connPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to create file %s: %w", connPath, err)
	}
	defer f.Close()

	if err := t.Execute(f, Details{
		SSID:     ssid,
		Password: password,
	}); err != nil {
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}
	added = append(added, connPath)

	for _, fileToWrite := range []struct {
		path     string
		contents []byte
		mode     os.FileMode
	}{{
		path:     NETWORK_MANAGER_CONF_WIFI_ON_FILE,
		contents: networkManagerWifiOnConfContents,
		mode:     0644,
	}, {
		path:     NETWORK_MANAGER_STATE_FILE,
		contents: networkManagerStateContents,
		mode:     0644,
	}} {
		if err := afero.WriteFile(nm.fs, fileToWrite.path, fileToWrite.contents, fileToWrite.mode); err != nil {
			return nil, fmt.Errorf("failed to write file %s: %w", fileToWrite.path, err)
		}
		added = append(added, fileToWrite.path)
	}

	return added, nil
}
