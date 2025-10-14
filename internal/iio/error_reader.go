package iio

type ErrorReader struct {
	error
}

func NewErrorReader(err error) ErrorReader {
	return ErrorReader{
		error: err,
	}
}

func (er ErrorReader) Read([]byte) (int, error) {
	return 0, er
}
