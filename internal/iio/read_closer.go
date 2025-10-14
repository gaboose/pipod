package iio

import "io"

type ReadCloser struct {
	io.Reader
	io.Closer
}
