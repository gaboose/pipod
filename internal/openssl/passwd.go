package openssl

import (
	"bytes"
	"fmt"
	"os/exec"
)

// Passwd runs openssl passwd.
func Passwd(password string) (string, error) {
	outbuf := bytes.NewBuffer(nil)
	errbuf := bytes.NewBuffer(nil)

	cmd := exec.Command("openssl", "passwd", "-6", password)
	cmd.Stdout = outbuf
	cmd.Stderr = errbuf

	if err := cmd.Run(); err != nil {
		return "", err
	}

	if errbuf.String() != "" {
		return "", fmt.Errorf("error: %s", errbuf.String())
	}

	return outbuf.String(), nil
}
