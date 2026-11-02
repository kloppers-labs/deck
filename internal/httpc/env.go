package httpc

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	envFile        = "http-client.env.json"
	privateEnvFile = "http-client.private.env.json"
	sharedEnv      = "$shared"
)

// envSet holds the environments from http-client.env.json, with the private
// file merged over it.
type envSet struct {
	names []string
	byEnv map[string]map[string]string
}

func parseEnvJSON(data []byte) (map[string]map[string]string, error) {
	var raw map[string]map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := make(map[string]map[string]string, len(raw))
	for env, vars := range raw {
		out[env] = map[string]string{}
		for k, v := range vars {
			switch v := v.(type) {
			case string:
				out[env][k] = v
			case float64, bool:
				out[env][k] = fmt.Sprint(v)
			}
		}
	}
	return out, nil
}

// loadEnvs uses the env files in the nearest directory from dir up to root
// that has one.
func loadEnvs(dir, root string) envSet {
	for d := dir; ; d = filepath.Dir(d) {
		if set, ok := readEnvDir(d); ok {
			return set
		}
		if d == root || !strings.HasPrefix(d, root) || filepath.Dir(d) == d {
			return envSet{}
		}
	}
}

func readEnvDir(dir string) (envSet, bool) {
	public, errPublic := readEnvFile(filepath.Join(dir, envFile))
	private, errPrivate := readEnvFile(filepath.Join(dir, privateEnvFile))
	if errPublic != nil && errPrivate != nil {
		return envSet{}, false
	}
	set := envSet{byEnv: map[string]map[string]string{}}
	for _, src := range []map[string]map[string]string{public, private} {
		for env, vars := range src {
			if set.byEnv[env] == nil {
				set.byEnv[env] = map[string]string{}
			}
			maps.Copy(set.byEnv[env], vars)
		}
	}
	for env := range set.byEnv {
		if env != sharedEnv {
			set.names = append(set.names, env)
		}
	}
	slices.Sort(set.names)
	return set, true
}

func readEnvFile(path string) (map[string]map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseEnvJSON(data)
}

// vars is $shared with the named environment over it.
func (e envSet) vars(name string) map[string]string {
	out := maps.Clone(e.byEnv[sharedEnv])
	if out == nil {
		out = map[string]string{}
	}
	if name != "" {
		maps.Copy(out, e.byEnv[name])
	}
	return out
}

// next cycles none → each environment → none.
func (e envSet) next(cur string) string {
	i := slices.Index(e.names, cur)
	if i+1 < len(e.names) {
		return e.names[i+1]
	}
	return ""
}
