package iio

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLastLine(t *testing.T) {
	testSingle := func(
		t *testing.T,
		input io.Reader,
		expectedLastLine string,
		expectedN int64,
		expectedErr error,
	) {
		w, lastLine := LastLine()
		n, err := io.Copy(w, input)
		assert.Equal(t, expectedErr, err)
		assert.Equal(t, expectedN, n)
		assert.Equal(t, expectedLastLine, lastLine.String())
	}

	t.Run("NewLineReturn", func(t *testing.T) {
		buf := bytes.NewBufferString("hello\n\rworld\n\r")
		testSingle(t, buf,
			"world",
			int64(buf.Len()),
			nil,
		)
	})

	t.Run("NewLine", func(t *testing.T) {
		buf := bytes.NewBufferString("hello\nworld\n")
		testSingle(t, buf, "world", int64(buf.Len()), nil)
	})

	t.Run("NoNewLine", func(t *testing.T) {
		buf := bytes.NewBufferString("hello\nworld")
		testSingle(t, buf, "world", int64(buf.Len()), nil)
	})

	t.Run("SingleLine", func(t *testing.T) {
		buf := bytes.NewBufferString("hello")
		testSingle(t, buf, "hello", int64(buf.Len()), nil)
	})

	t.Run("Empty", func(t *testing.T) {
		buf := bytes.NewBuffer(nil)
		testSingle(t, buf, "", int64(buf.Len()), nil)
	})

	t.Run("Error", func(t *testing.T) {
		buf := bytes.NewBufferString("hello")
		terr := errors.New("test error")
		r := Reader(func(p []byte) (n int, err error) {
			if buf.Len() > 0 {
				return buf.Read(p)
			} else {
				return 0, terr
			}
		})

		testSingle(t, r, "hello", int64(buf.Len()), terr)
	})

}
