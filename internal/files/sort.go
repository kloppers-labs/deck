package files

import (
	"path/filepath"
	"sort"
	"strings"
)

type SortMode int

const (
	SortName SortMode = iota
	SortModified
	SortSize
	SortExt
	numSortModes
)

var sortNames = [numSortModes]string{"name", "modified", "size", "extension"}

func (s SortMode) String() string { return sortNames[s] }

func (s SortMode) Next() SortMode { return (s + 1) % numSortModes }

func byName(a, b *Node) bool {
	la, lb := strings.ToLower(a.Name), strings.ToLower(b.Name)
	if la != lb {
		return la < lb
	}
	return a.Name < b.Name
}

// sortNodes orders directories first, then by mode, falling back to the name.
func sortNodes(nodes []*Node, mode SortMode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		switch mode {
		case SortModified:
			if !a.ModTime.Equal(b.ModTime) {
				return a.ModTime.After(b.ModTime)
			}
		case SortSize:
			if !a.IsDir && a.Size != b.Size {
				return a.Size > b.Size
			}
		case SortExt:
			ea, eb := strings.ToLower(filepath.Ext(a.Name)), strings.ToLower(filepath.Ext(b.Name))
			if !a.IsDir && ea != eb {
				return ea < eb
			}
		}
		return byName(a, b)
	})
}
