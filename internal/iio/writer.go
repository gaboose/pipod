package iio

type Writer func(p []byte) (n int, err error)

func (w Writer) Write(p []byte) (n int, err error) { return w(p) }

func (w Writer) WithPrefix(s string) Writer {
	prefixBts := append([]byte(s), ' ')
	writePrefix := true

	return func(oldPayload []byte) (n int, err error) {
		p := make([]byte, 0, len(oldPayload))
		nMap := make([]int, 1, len(oldPayload))

		for _, b := range oldPayload {
			if b == '\r' {
				writePrefix = true
			} else if writePrefix {
				p = append(p, prefixBts...)
				nMap = appendNMap(nMap, false, len(prefixBts))
				writePrefix = false
			}

			p = append(p, b)
			nMap = appendNMap(nMap, true, 1)

			if b == '\n' {
				writePrefix = true
			}
		}

		n, err = w(p)
		return nMap[n], err
	}
}

func appendNMap(nMap []int, incOld bool, newN int) []int {
	oldN := nMap[len(nMap)-1]
	if incOld {
		oldN++
	}
	for range newN {
		nMap = append(nMap, oldN)
	}
	return nMap
}
