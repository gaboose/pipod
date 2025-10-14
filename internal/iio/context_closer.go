package iio

import "context"

func ContextCloser() (context.Context, Closer) {
	ctx, cancel := context.WithCancel(context.Background())
	return ctx, Closer(func() error { cancel(); return nil })
}
