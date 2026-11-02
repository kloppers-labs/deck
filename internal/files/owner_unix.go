//go:build unix

package files

import (
	"io/fs"
	"os/user"
	"strconv"
	"sync"
	"syscall"
)

var owners sync.Map

func ownerOf(fi fs.FileInfo) string {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	uid := strconv.FormatUint(uint64(st.Uid), 10)
	if name, ok := owners.Load(uid); ok {
		return name.(string)
	}
	name := uid
	if u, err := user.LookupId(uid); err == nil {
		name = u.Username
	}
	owners.Store(uid, name)
	return name
}
