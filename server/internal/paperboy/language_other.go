//go:build !windows

package paperboy

// systemLanguage is only implemented for Windows; elsewhere the environment
// variables decide.
func systemLanguage() string { return "" }
