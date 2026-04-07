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
	Expanded  bool
}

// Selector is a multi-select list component.
type Selector struct {
	Items  []SelectItem
	cursor int
	Title  string
}

func NewSelector(title string, items []SelectItem) Selector {
	return Selector{
		Title: title,
		Items: items,
	}
}

type SelectorMsg struct{}

func (s *Selector) ToggleCurrent() {
	if s.cursor >= len(s.Items) {
		return
	}
	item := &s.Items[s.cursor]
	if item.Disabled {
		return
	}
	item.Selected = !item.Selected
	// Toggle children
	for i := range item.Children {
		item.Children[i].Selected = item.Selected
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
	}
	return false
}

func (s *Selector) Update(msg tea.Msg) (Selector, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if s.cursor > 0 {
				s.cursor--
			}
		case "down", "j":
			if s.cursor < len(s.Items)-1 {
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
	return *s, nil
}

func (s Selector) View() string {
	var sb strings.Builder
	for i, item := range s.Items {
		cursor := "  "
		if i == s.cursor {
			cursor = StyleFocused.Render(MarkerFocused + " ")
		}

		checkmark := MarkerEmpty
		if item.Disabled {
			checkmark = StyleMuted.Render(MarkerEmpty)
		} else if item.Selected {
			checkmark = StyleSelected.Render(MarkerSelected)
		}

		label := item.Label
		if item.Disabled {
			label = StyleMuted.Render(label + " (not detected)")
		} else if i == s.cursor {
			label = StyleFocused.Render(label)
		}

		detail := ""
		if item.Detail != "" {
			detail = "  " + StyleMuted.Render(item.Detail)
		}

		sizeStr := ""
		if item.SizeBytes > 0 {
			sizeStr = "  " + StyleSizeHint.Render(FormatSize(item.SizeBytes))
		}

		sb.WriteString(fmt.Sprintf("%s%s %s%s%s\n", cursor, checkmark, label, detail, sizeStr))

		// Render children indented
		for _, child := range item.Children {
			childCheck := MarkerEmpty
			if child.Selected {
				childCheck = StyleSelected.Render(MarkerSelected)
			}
			childLabel := "  " + child.Label
			sb.WriteString(fmt.Sprintf("    %s %s\n", childCheck, StyleMuted.Render(childLabel)))
		}
	}
	return sb.String()
}
