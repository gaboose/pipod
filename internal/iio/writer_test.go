package iio

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPrefix(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	pw := Writer(buf.Write).WithPrefix("A")

	n, err := fmt.Fprintf(pw, "B")
	assert.Equal(t, 1, n)
	assert.Nil(t, err)

	n, err = fmt.Fprintf(pw, "B\n")
	assert.Equal(t, 2, n)
	assert.Nil(t, err)

	n, err = fmt.Fprintln(pw, "CC")
	assert.Equal(t, 3, n)
	assert.Nil(t, err)

	assert.Equal(t, "A BB\nA CC\n", buf.String())
}
