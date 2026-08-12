package smate

import "testing"

// The version is shown in the banner and read by whatever releases the tool, so
// what the file holds has to be one clean line: a stray newline or a blank
// VERSION would print as "v" and nobody would notice until it shipped.
func TestVersionIsOneCleanLine(t *testing.T) {
	v := Version()
	if v == "" {
		t.Fatal("VERSION is empty")
	}
	for _, r := range v {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			t.Errorf("VERSION holds whitespace: %q", v)
			break
		}
	}
}
