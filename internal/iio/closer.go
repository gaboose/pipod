package iio

import "io"

type Closer func() error

func (c Closer) Close() error { return c() }

func (left Closer) ChainCloser(right io.Closer) Closer {
	return func() error {
		return firstNonNil(left.Close(), right.Close())
	}
}

func (c Closer) WithReader(r io.Reader) ReadCloser {
	return ReadCloser{
		Reader: r,
		Closer: c,
	}
}

func (c Closer) WithReadCloser(rc io.ReadCloser) ReadCloser {
	return ReadCloser{
		Reader: rc,
		Closer: c.ChainCloser(rc),
	}
}

func (c Closer) WithErrReader(err error) ReadCloser {
	return ReadCloser{
		Reader: NewErrorReader(err),
		Closer: c,
	}
}

func firstNonNil(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
