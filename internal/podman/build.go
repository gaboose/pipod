package podman

import (
	"bufio"
	"io"
	"os/exec"
)

// Build runs podman build.
func Build(platform string) (*Image, error) {
	cmd := exec.Command("podman", "build", ".", "--platform", platform)
	cmd.Stderr = stderr

	stdoutReader, stdoutWriter := io.Pipe()
	cmd.Stdout = io.MultiWriter(stdout, stdoutWriter)
	lastLineCh := startLastLineReader(stdoutReader)

	if err := cmd.Run(); err != nil {
		stdoutWriter.Close()
		return nil, err
	}
	stdoutWriter.CloseWithError(io.EOF)

	return &Image{name: <-lastLineCh}, nil
}

func startLastLineReader(r io.Reader) <-chan string {
	ch := make(chan string, 1)
	go func() {
		ch <- lastLine(r)
	}()
	return ch
}

func lastLine(r io.Reader) string {
	var last string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		last = scanner.Text()
	}
	return last
}
