package guestfish

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/gaboose/pipod/internal/iio"
)

const (
	green  = "\033[32m"
	reset  = "\033[0m"
	prefix = green + "[guestfish]" + reset
)

var stderr = iio.Writer(os.Stderr.Write).WithPrefix(prefix)

func TarOut(image string, partition string) io.ReadCloser {
	ctx, closer := iio.ContextCloser()
	guestfishCmd := exec.CommandContext(ctx, "guestfish", "--ro", "-a", image, "-m", partition, "--", "tar-out", "/", "-")
	guestfishCmd.Stderr = stderr

	reader, writer := io.Pipe()
	guestfishCmd.Stdout = writer

	go func() {
		if err := guestfishCmd.Run(); err != nil {
			writer.CloseWithError(fmt.Errorf("failed to run cmd: %w", err))
		} else {
			writer.Close()
		}
	}()

	return iio.ReadCloser{
		Reader: reader,
		Closer: closer,
	}
}
