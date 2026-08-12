package patch

import (
	"bytes"
	"sort"
)

// minValueLen is the shortest value worth searching for: a key set to "test" would
// otherwise block every patch that happens to use the word.
const minValueLen = 8

// ScanValues reports the names of values that appear verbatim in the patch. Names,
// never the values: this ends up in an error message.
//
// Exact substring matching catches the accident — a key copied into a config file
// — but not intent: base64 or a value written in pieces gets through. Against that
// only a credential broker helps.
func ScanValues(data []byte, values map[string]string) []string {
	var found []string
	for name, v := range values {
		if len(v) < minValueLen {
			continue
		}
		if bytes.Contains(data, []byte(v)) {
			found = append(found, name)
		}
	}
	sort.Strings(found)
	return found
}
