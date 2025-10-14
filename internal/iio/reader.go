package iio

type Reader func(p []byte) (n int, err error)

func (r Reader) Read(p []byte) (n int, err error) { return r(p) }
