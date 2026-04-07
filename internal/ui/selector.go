package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// SelectItem is a single item in the multi-select list.
type SelectItem struct {
	Label     string
	Detail    string
	SizeBytes int64
	Selected  bool
	Disabled  bool
	Children  []SelectItem
}

// flatItem is used internally to map cursor position to actual item.
type flatItem struct {
	parentIdx int // -1 if top-level
	childIdx  int // index within parent.Children, -1 if top-level
}

// Selector is a multi-select list component with optional child items.
type Selector struct {
	Items  []SelectItem
	cursor int
	flat   []flatItem // flattened navigation index
	Title  string
}

func NewSelector(title string, items []SelectItem) Selector {
	s := Selector{Title: title, Items: items}
	s.rebuildFlat()
	return s
}

// rebuildFlat rebuilds the flat navigation list after Items change.
func (s *Selector) rebuildFlat() {
	s.flat = nil
	for i, item := range s.Items {
		s.flat = append(s.flat, flatItem{parentIdx: i, childIdx: -1})
		if len(item.Children) > 0 {
			for j := range item.Children {
				s.flat = append(s.flat, flatItem{parentIdx: i, childIdx: j})
			}
		}
	}
	if s.cursor >= len(s.flat) {
		s.cursor = 0
	}
}

func (s *Selector) currentItem() (parent *SelectItem, child *SelectItem) {
	if len(s.flat) == 0 || s.cursor >= len(s.flat) {
		return nil, nil
	}
	fi := s.flat[s.cursor]
	p := &s.Items[fi.parentIdx]
	if fi.childIdx >= 0 {
		return p, &p.Children[fi.childIdx]
	}
	return p, nil
}

func (s *Selector) ToggleCurrent() {
	parent, child := s.currentItem()
	if parent == nil || parent.Disabled {
		return
	}
	if child != nil {
		// Toggle individual child
		child.Selected = !child.Selected
		// Update parent state: selected if all children selected, deselected if none
		allSel := true
		anySel := false
		for _, c := range parent.Children {
			if c.Selected {
				anySel = true
			} else {
				allSel = false
			}
		}
		parent.Selected = allSel || anySel // partial counts as "on" for parent display
		_ = allSel
	} else {
		// Toggle parent — also toggle all children
		parent.Selected = !parent.Selected
		for i := range parent.Children {
			parent.Children[i].Selected = parent.Selected
		}
	}
}

func (s *Selector) SelectAll() {
	for i := range s.Items {
		if !s.Items[i].Disabled {
			s.Items[i].Selected = true
			for j := range s.Items[i].Children {
				s.Items[i].Children[j].Selected = true
			}
		}
	}
}

func (s *Selector) SelectNone() {
	for i := range s.Items {
		s.Items[i].Selected = false
		for j := range s.Items[i].Children {
			s.Items[i].Children[j].Selected = false
		}
	}
}

func (s *Selector) AnySelected() bool {
	for _, item := range s.Items {
		if item.Selected {
			return true
		}
		for _, c := range item.Children {
			if c.Selected {
				return true
			}
		}
	}
	return false
}

// SelectedTopLevel returns indices of top-level items that are selected (or have selected children).
func (s *Selector) SelectedTopLevel() []int {
	var result []int
	for i, item := range s.Items {
		if item.Selected {
			result = append(result, i)
			continue
		}
		for _, c := range item.Children {
			if c.Selected {
				result = append(result, i)
				break
			}
		}
	}
	return result
}

// SelectedChildren returns selected child labels for the given parent index.
func (s *Selector) SelectedChildren(parentIdx int) []string {
	if parentIdx >= len(s.Items) {
		return nil
	}
	var result []string
	for _, c := range s.Items[parentIdx].Children {
		if c.Selected {
			result = append(result, c.Label)
		}
	}
	return result
}

func (s Selector) Update(msg tea.Msg) (Selector, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if s.cursor > 0 {
				s.cursor--
			}
		case "down", "j":
			if s.cursor < len(s.flat)-1 {
				s.cursor++
			}
		case " ":
			s.ToggleCurrent()
		case "a":
			s.SelectAll()
		case "n":
			s.SelectNone()
		}
	}
	return s, nil
}

func (s Selector) View() string {
	var sb strings.Builder
	for fi, fitem := range s.flat {
		focused := fi == s.cursor
		isChild := fitem.childIdx >= 0

		var item *SelectItem
		if isChild {
			item = &s.Items[fitem.parentIdx].Children[fitem.childIdx]
		} else {
			item = &s.Items[fitem.parentIdx]
		}

		// Indentation
		indent := ""
		if isChild {
			indent = "    "
		}

		// Cursor marker
		cursorStr := "  "
		if focused {
			cursorStr = StyleFocused.Render("› ")
		}

		// Checkbox
		var checkmark string
		switch {
		case item.Disabled:
			checkmark = StyleMuted.Render(MarkerEmpty)
		case isChild && item.Selected:
			checkmark = StyleSelected.Render(MarkerSelected)
		case isChild && !item.Selected:
			checkmark = MarkerEmpty
		default:
			// Top-level: check if partial (some children selected)
			parent := &s.Items[fitem.parentIdx]
			if len(parent.Children) > 0 {
				allSel, anySel := true, false
				for _, c := range parent.Children {
					if c.Selected {
						anySel = true
					} else {
						allSel = false
					}
				}
				if allSel {
					checkmark = StyleSelected.Render(MarkerSelected)
				} else if anySel {
					checkmark = StyleWarning.Render(MarkerPartial)
				} else {
					checkmark = MarkerEmpty
				}
			} else if item.Selected {
				checkmark = StyleSelected.Render(MarkerSelected)
			} else {
				checkmark = MarkerEmpty
			}
		}

		// Label
		label := item.Label
		if item.Disabled {
			label = StyleMuted.Render(label + " (not detected)")
		} else if focused {
			label = StyleFocused.Render(label)
		} else if isChild {
			label = StyleMuted.Render(label)
		}

		// Detail
		detail := ""
		if item.Detail != "" && !isChild {
			detail = "  " + StyleMuted.Render(item.Detail)
		}

		// Size
		sizeStr := ""
		if item.SizeBytes > 0 {
			sizeStr = "  " + StyleSizeHint.Render(FormatSize(item.SizeBytes))
		}

		sb.WriteString(fmt.Sprintf("%s%s%s %s%s%s\n", indent, cursorStr, checkmark, label, detail, sizeStr))
	}
	return sb.String()
}
