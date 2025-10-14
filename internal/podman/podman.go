package podman

import (
	"os"

	"github.com/gaboose/pipod/internal/iio"
)

const (
	green  = "\033[32m"
	reset  = "\033[0m"
	prefix = green + "[podman]" + reset
)

var stdout = iio.Writer(os.Stdout.Write).WithPrefix(prefix)
var stderr = iio.Writer(os.Stderr.Write).WithPrefix(prefix)
