package glibkeyfile

import "strings"

// EscapeValue escapes a string for use as a GLib key file value.
func EscapeValue(s string) string {
	var b strings.Builder
	for i, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == ' ' && i == 0:
			// Leading whitespace is stripped unless escaped.
			b.WriteString(`\s`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
