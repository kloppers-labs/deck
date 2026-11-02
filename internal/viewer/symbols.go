package viewer

import "strings"

var kindGlyphs = map[SymbolKind]string{
	SymbolFile:          "▤",
	SymbolModule:        "▣",
	SymbolNamespace:     "▣",
	SymbolPackage:       "▣",
	SymbolClass:         "◆",
	SymbolMethod:        "ƒ",
	SymbolProperty:      "◇",
	SymbolField:         "▪",
	SymbolConstructor:   "⊕",
	SymbolEnum:          "▦",
	SymbolInterface:     "◈",
	SymbolFunction:      "ƒ",
	SymbolVariable:      "ν",
	SymbolConstant:      "π",
	SymbolEnumMember:    "▫",
	SymbolStruct:        "◼",
	SymbolEvent:         "↯",
	SymbolOperator:      "±",
	SymbolTypeParameter: "τ",
	SymbolHeading:       "#",
}

// KindGlyph is a one-cell marker for a symbol kind.
func KindGlyph(k SymbolKind) string {
	if g, ok := kindGlyphs[k]; ok {
		return g
	}
	return "•"
}

// SymbolLabel is an outline row: the name indented by depth after its glyph.
func SymbolLabel(s Symbol) string {
	return strings.Repeat("  ", max(s.Depth, 0)) + KindGlyph(s.Kind) + " " + s.Name
}

// Breadcrumb names the innermost symbol containing p, outermost first.
func Breadcrumb(syms []Symbol, p Pos) []string {
	best := -1
	for i, s := range syms {
		if s.Range.contains(p) && (best < 0 || s.Depth >= syms[best].Depth) {
			best = i
		}
	}
	var out []string
	for i := best; i >= 0; {
		if syms[i].Kind != SymbolFile {
			out = append(out, syms[i].Name)
		}
		next := syms[i].Parent
		if next >= i {
			break
		}
		i = next
	}
	for l, r := 0, len(out)-1; l < r; l, r = l+1, r-1 {
		out[l], out[r] = out[r], out[l]
	}
	return out
}

// MarkdownOutline lists the ATX headings outside fenced code blocks, nested by
// level, each spanning to the next heading of the same or a higher level.
func MarkdownOutline(raw []string) []Symbol {
	type heading struct{ line, level int }
	var hs []heading
	var names []string
	fence := ""
	for i, l := range raw {
		t := strings.TrimLeft(l, " ")
		if len(l)-len(t) > 3 {
			continue
		}
		if fence != "" {
			if strings.HasPrefix(t, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fence = t[:3]
			continue
		}
		level := len(t) - len(strings.TrimLeft(t, "#"))
		if level < 1 || level > 6 || (len(t) > level && t[level] != ' ' && t[level] != '\t') {
			continue
		}
		name := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(t[level:]), "#"))
		hs = append(hs, heading{i, level})
		names = append(names, name)
	}
	out := make([]Symbol, len(hs))
	var stack []int
	for i, h := range hs {
		for len(stack) > 0 && hs[stack[len(stack)-1]].level >= h.level {
			stack = stack[:len(stack)-1]
		}
		parent := -1
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		end := len(raw) - 1
		for _, n := range hs[i+1:] {
			if n.level <= h.level {
				end = n.line - 1
				break
			}
		}
		start := Pos{h.line, 0}
		out[i] = Symbol{
			Name: names[i], Kind: SymbolHeading, Depth: len(stack), Parent: parent,
			Range: Range{start, Pos{end, len([]rune(raw[end]))}}, Selection: Range{start, start},
		}
		stack = append(stack, i)
	}
	return out
}
