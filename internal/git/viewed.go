package git

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
)

const deletedHash = "deleted"

// BlobHash is git's object id for data as a blob.
func BlobHash(data []byte) string {
	h := sha1.New()
	h.Write([]byte("blob " + strconv.Itoa(len(data)) + "\x00"))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// FileHash is the blob hash of path's working-tree content, or "deleted".
func FileHash(top, path string) string {
	data, err := os.ReadFile(filepath.Join(top, filepath.FromSlash(path)))
	if err != nil {
		return deletedHash
	}
	return BlobHash(data)
}

// Viewed records the files ticked in a review, by the content they had then.
type Viewed struct {
	Base  string            `json:"base"`
	Files map[string]string `json:"files"`
}

func viewedPath(gitDir string) string { return filepath.Join(gitDir, "deck", "review.json") }

func LoadViewed(gitDir string) Viewed {
	var v Viewed
	if data, err := os.ReadFile(viewedPath(gitDir)); err == nil {
		json.Unmarshal(data, &v)
	}
	if v.Files == nil {
		v.Files = map[string]string{}
	}
	return v
}

// ForBase keeps the ticks for base and starts a fresh set for any other base.
func (v Viewed) ForBase(base string) Viewed {
	if v.Base == base && v.Files != nil {
		return v
	}
	return Viewed{Base: base, Files: map[string]string{}}
}

func (v Viewed) Is(path, hash string) bool {
	stored, ok := v.Files[path]
	return ok && hash != "" && stored == hash
}

func (v Viewed) Save(gitDir string) error {
	dir := filepath.Dir(viewedPath(gitDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".review-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), viewedPath(gitDir))
}
