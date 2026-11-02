package ui

import "runtime"

var altPrefix = altPrefixFor(runtime.GOOS)

func altPrefixFor(goos string) string {
	if goos == "darwin" {
		return "⌥"
	}
	return "alt+"
}

// Alt labels key k with the platform's Alt modifier: ⌥k on macOS, alt+k elsewhere.
func Alt(k string) string { return altPrefix + k }
