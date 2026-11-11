package main

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var versionFile string

// version is the plugin's own version, embedded from the VERSION file next to
// this source. Raise it when the plugin changes: the plugin zip names the
// program <plugin>-v<version>, and a download only installs a higher one
// (ADR 0075).
func version() string { return strings.TrimSpace(versionFile) }
