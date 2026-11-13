package control

import (
	"cmp"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

const probeTimeout = 200 * time.Millisecond

type Instance struct {
	PID     int       `json:"pid"`
	Root    string    `json:"root"`
	Cwd     string    `json:"cwd"`
	Version string    `json:"version"`
	Socket  string    `json:"socket"`
	Started time.Time `json:"started"`
	Focused time.Time `json:"focused,omitzero"`
}

// Register writes inst's sidecar; cleanup removes it and the socket.
func Register(dir string, inst Instance) (func(), error) {
	if err := ensureDir(dir); err != nil {
		return nil, err
	}
	if err := writeSidecar(dir, inst); err != nil {
		return nil, err
	}
	return func() {
		os.Remove(filepath.Join(dir, sidecarName(inst.PID)))
		os.Remove(inst.Socket)
	}, nil
}

func writeSidecar(dir string, inst Instance) error {
	data, err := json.Marshal(inst)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".sidecar-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, sidecarName(inst.PID)))
}

func readSidecar(path string) (Instance, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Instance{}, err
	}
	var inst Instance
	err = json.Unmarshal(data, &inst)
	return inst, err
}

func Touch(dir string, pid int, at time.Time) error {
	inst, err := readSidecar(filepath.Join(dir, sidecarName(pid)))
	if err != nil {
		return err
	}
	inst.Focused = at
	return writeSidecar(dir, inst)
}

// List returns the live instances in dir and removes the files of dead ones.
func List(dir string) ([]Instance, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	var out []Instance
	for _, p := range paths {
		inst, err := readSidecar(p)
		if err != nil || inst.PID <= 0 || !alive(inst) {
			os.Remove(p)
			if err == nil && inst.Socket != "" {
				os.Remove(inst.Socket)
			}
			continue
		}
		out = append(out, inst)
	}
	return out, nil
}

func alive(inst Instance) bool {
	if err := syscall.Kill(inst.PID, 0); err != nil && !errors.Is(err, syscall.EPERM) {
		return false
	}
	c, err := net.DialTimeout("unix", inst.Socket, probeTimeout)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func under(root, path string) bool {
	if root == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

// Pick chooses the instance to talk to: an explicit pid, else an exact root,
// else the deepest root containing target; ties go to the most recently focused, then started.
func Pick(insts []Instance, pid int, root, target string) (Instance, bool) {
	if pid > 0 {
		i := slices.IndexFunc(insts, func(in Instance) bool { return in.PID == pid })
		if i < 0 {
			return Instance{}, false
		}
		return insts[i], true
	}
	var exact, containing []Instance
	for _, in := range insts {
		switch {
		case root != "" && in.Root == root:
			exact = append(exact, in)
		case under(in.Root, target):
			containing = append(containing, in)
		}
	}
	for _, set := range [][]Instance{exact, containing} {
		if len(set) == 0 {
			continue
		}
		return slices.MaxFunc(set, func(a, b Instance) int {
			return cmp.Or(
				cmp.Compare(len(a.Root), len(b.Root)),
				a.Focused.Compare(b.Focused),
				a.Started.Compare(b.Started),
			)
		}), true
	}
	return Instance{}, false
}
