package iio

import (
	"bytes"
)

// LastLine returns a writer and a buffer that contains the last line written to the
// writer. The buffer is not concurrency-safe - it is the responsibility of the user
// to not concurrently access the buffer and write to the writer.
func LastLine() (Writer, *bytes.Buffer) {
	lastLine := bytes.NewBuffer(nil)
	var reset bool

	return Writer(func(p []byte) (n int, err error) {
		for i, b := range p {
			if b == '\n' || b == '\r' {
				reset = true
				continue
			} else if reset {
				lastLine.Reset()
				reset = false
			}

			if lerr := lastLine.WriteByte(b); lerr != nil {
				return i, lerr
			}
		}

		return len(p), nil
	}), lastLine
}
