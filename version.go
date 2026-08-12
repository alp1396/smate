// Package smate is the root of the module and holds the one thing the whole tool
// has to agree on: its own version number.
//
// It is embedded rather than passed with -ldflags, so every way of building — make
// build, go build, go install, go test — produces the same answer. A flag can be
// forgotten; an embedded file cannot.
package smate

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var versionFile string

func Version() string { return strings.TrimSpace(versionFile) }
