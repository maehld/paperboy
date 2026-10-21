//go:build windows

package paperboy

import "syscall"

var getUserDefaultUILanguage = syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage")

// systemLanguage reads the Windows UI language.
func systemLanguage() string {
	r, _, _ := getUserDefaultUILanguage.Call()
	switch uint16(r) & 0x3ff {
	case 0x07: // German
		return "de"
	case 0x09: // English
		return "en"
	}
	return ""
}
