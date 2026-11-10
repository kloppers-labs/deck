package git

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/kloppers-labs/deck/internal/ui"
)

// Section is one group of the change list. An entry changed in both the index
// and the work tree appears in Staged and Unstaged, each with its own diff.
type Section int

const (
	SectionStaged Section = iota
	SectionUnstaged
	SectionUntracked
	SectionConflicts
	numSections
)

var sectionNames = [numSections]string{"Staged", "Unstaged", "Untracked", "Conflicts"}

func (s Section) String() string { return sectionNames[s] }

// Item is an entry as listed in one section.
type Item struct {
	Section Section
	Entry   Entry
}

// Items groups entries by section, in section order, ignoring ignored entries.
func Items(entries []Entry) []Item {
	var buckets [numSections][]Item
	for _, e := range entries {
		switch {
		case e.Conflict():
			buckets[SectionConflicts] = append(buckets[SectionConflicts], Item{SectionConflicts, e})
		case e.Untracked():
			buckets[SectionUntracked] = append(buckets[SectionUntracked], Item{SectionUntracked, e})
		default:
			if e.Staged() {
				buckets[SectionStaged] = append(buckets[SectionStaged], Item{SectionStaged, e})
			}
			if e.Unstaged() {
				buckets[SectionUnstaged] = append(buckets[SectionUnstaged], Item{SectionUnstaged, e})
			}
		}
	}
	var out []Item
	for _, b := range buckets {
		out = append(out, b...)
	}
	return out
}

// Code is the status letter(s) the item shows in its section.
func (it Item) Code() string {
	switch it.Section {
	case SectionStaged:
		return string(it.Entry.X)
	case SectionUnstaged:
		return string(it.Entry.Y)
	case SectionUntracked:
		return "?"
	}
	return string([]byte{it.Entry.X, it.Entry.Y})
}

func (it Item) label() string {
	if it.Entry.Orig != "" && it.Section == SectionStaged {
		return it.Entry.Orig + " → " + it.Entry.Path
	}
	return it.Entry.Path
}

func sectionCounts(items []Item) [numSections]int {
	var n [numSections]int
	for _, it := range items {
		n[it.Section]++
	}
	return n
}

// row is one line of the change list: a section header or an item.
type row struct {
	header bool
	sec    Section
	count  int
	item   int
}

func layoutRows(items []Item) []row {
	counts := sectionCounts(items)
	var rows []row
	for i, it := range items {
		if i == 0 || items[i-1].Section != it.Section {
			rows = append(rows, row{header: true, sec: it.Section, count: counts[it.Section]})
		}
		rows = append(rows, row{sec: it.Section, item: i})
	}
	return rows
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// headerLine renders `⎇ branch → upstream ↑a ↓b · n staged · m unstaged`.
func headerLine(s Snapshot) string {
	var b strings.Builder
	b.WriteString(ui.Active.Render("⎇ " + s.Branch))
	if s.Upstream != "" {
		b.WriteString(ui.Muted.Render(" → " + s.Upstream))
		fmt.Fprintf(&b, " ↑%d ↓%d", s.Ahead, s.Behind)
	}
	counts := sectionCounts(Items(s.Entries))
	var parts []string
	if n := counts[SectionStaged]; n > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(ui.ColorString).Render(fmt.Sprintf("%d staged", n)))
	}
	if n := counts[SectionUnstaged]; n > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(ui.ColorWarn).Render(fmt.Sprintf("%d unstaged", n)))
	}
	if n := counts[SectionUntracked]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d untracked", n))
	}
	if n := counts[SectionConflicts]; n > 0 {
		parts = append(parts, ui.Err.Render(plural(n, "conflict")))
	}
	if len(parts) == 0 {
		parts = append(parts, ui.Muted.Render("clean"))
	}
	for _, p := range parts {
		b.WriteString(" · " + p)
	}
	return b.String()
}
