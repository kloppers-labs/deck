package lsp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func decodeLocations(raw json.RawMessage) ([]Location, error) {
	raw = bytes.TrimSpace(raw)
	if isNull(raw) {
		return nil, nil
	}
	var items []json.RawMessage
	switch raw[0] {
	case '[':
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
	case '{':
		items = []json.RawMessage{raw}
	default:
		return nil, fmt.Errorf("lsp: unexpected location result %.40s", raw)
	}
	var out []Location
	for _, item := range items {
		var probe struct {
			URI                  string `json:"uri"`
			Range                Range  `json:"range"`
			TargetURI            string `json:"targetUri"`
			TargetSelectionRange Range  `json:"targetSelectionRange"`
		}
		if err := json.Unmarshal(item, &probe); err != nil {
			return nil, err
		}
		if probe.TargetURI != "" {
			out = append(out, Location{URI: probe.TargetURI, Range: probe.TargetSelectionRange})
			continue
		}
		out = append(out, Location{URI: probe.URI, Range: probe.Range})
	}
	return out, nil
}

func decodeHover(raw json.RawMessage) (Hover, error) {
	if isNull(raw) {
		return Hover{}, nil
	}
	var h struct {
		Contents json.RawMessage `json:"contents"`
		Range    *Range          `json:"range"`
	}
	if err := json.Unmarshal(raw, &h); err != nil {
		return Hover{}, err
	}
	md, err := markedText(h.Contents)
	if err != nil {
		return Hover{}, err
	}
	return Hover{Markdown: md, Range: h.Range}, nil
}

func markedText(raw json.RawMessage) (string, error) {
	raw = bytes.TrimSpace(raw)
	if isNull(raw) {
		return "", nil
	}
	switch raw[0] {
	case '"':
		var s string
		err := json.Unmarshal(raw, &s)
		return s, err
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return "", err
		}
		parts := make([]string, 0, len(items))
		for _, item := range items {
			s, err := markedText(item)
			if err != nil {
				return "", err
			}
			if s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n\n"), nil
	}
	var obj struct {
		Kind     string  `json:"kind"`
		Language *string `json:"language"`
		Value    string  `json:"value"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", err
	}
	if obj.Language != nil {
		return "```" + *obj.Language + "\n" + obj.Value + "\n```", nil
	}
	return obj.Value, nil
}

func decodeSymbols(raw json.RawMessage, uri string) ([]Symbol, error) {
	if isNull(raw) {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	var probe struct {
		Location *json.RawMessage `json:"location"`
	}
	if err := json.Unmarshal(items[0], &probe); err != nil {
		return nil, err
	}
	if probe.Location != nil {
		var flat []SymbolInformation
		if err := json.Unmarshal(raw, &flat); err != nil {
			return nil, err
		}
		return nestFlat(flat), nil
	}
	var tree []DocumentSymbol
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, err
	}
	var out []Symbol
	var walk func(nodes []DocumentSymbol, depth, parent int, container string)
	walk = func(nodes []DocumentSymbol, depth, parent int, container string) {
		for _, n := range nodes {
			out = append(out, Symbol{
				Name: n.Name, Detail: n.Detail, Kind: n.Kind, URI: uri,
				Range: n.Range, SelectionRange: n.SelectionRange,
				Container: container, Depth: depth, Parent: parent,
			})
			walk(n.Children, depth+1, len(out)-1, n.Name)
		}
	}
	walk(tree, 0, -1, "")
	return out, nil
}

func nestFlat(flat []SymbolInformation) []Symbol {
	sort.SliceStable(flat, func(i, j int) bool {
		a, b := flat[i].Location.Range, flat[j].Location.Range
		if a.Start != b.Start {
			return a.Start.Before(b.Start)
		}
		return b.End.Before(a.End)
	})
	out := make([]Symbol, 0, len(flat))
	var stack []int
	for _, s := range flat {
		r := s.Location.Range
		for len(stack) > 0 && !out[stack[len(stack)-1]].Range.Contains(r) {
			stack = stack[:len(stack)-1]
		}
		parent := -1
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		out = append(out, Symbol{
			Name: s.Name, Kind: s.Kind, URI: s.Location.URI,
			Range: r, SelectionRange: r, Container: s.ContainerName,
			Depth: len(stack), Parent: parent,
		})
		stack = append(stack, len(out)-1)
	}
	return out
}

func decodeWorkspaceSymbols(raw json.RawMessage) ([]Symbol, error) {
	if isNull(raw) {
		return nil, nil
	}
	var flat []SymbolInformation
	if err := json.Unmarshal(raw, &flat); err != nil {
		return nil, err
	}
	out := make([]Symbol, 0, len(flat))
	for _, s := range flat {
		out = append(out, Symbol{
			Name: s.Name, Kind: s.Kind, URI: s.Location.URI,
			Range: s.Location.Range, SelectionRange: s.Location.Range,
			Container: s.ContainerName, Parent: -1,
		})
	}
	return out, nil
}
