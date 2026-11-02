//go:build !unix

package files

import "io/fs"

func ownerOf(fs.FileInfo) string { return "" }
