package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ichava/maintainer-toolkit/src/internal/core/config"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// menuIndex finds a menu item by label, so the tests do not hard-code the
// order the items happen to be listed in.
func menuIndex(t *testing.T, m *Model, label string) int {
	t.Helper()
	for i, item := range m.items {
		if item.label == label {
			return i
		}
	}
	t.Fatalf("no menu item %q", label)
	return -1
}

func TestMenuActions(t *testing.T) {
	packs := []*config.PackConfig{
		{Name: "icon-sets-tabler", Pack: "ichava/icon-sets-tabler"},
		{Name: "icon-sets-flag", Pack: "ichava/icon-sets-flag"},
	}

	tests := []struct {
		name   string
		item   string
		keys   []string
		want   *Action
		screen screen
	}{
		{
			name: "sync all is a dry run, as in the Python menu",
			item: "Sync all packs (dry-run)",
			want: &Action{All: true, DryRun: true},
		},
		{
			name: "sync one pack asks, and y confirms",
			item: "Sync one pack",
			keys: []string{"down", "enter", "y"},
			want: &Action{Pack: "icon-sets-flag"},
		},
		{
			name:   "n declines and goes back to the list",
			item:   "Sync one pack",
			keys:   []string{"enter", "n"},
			want:   nil,
			screen: screenPacks,
		},
		{
			name:   "the default is no: enter on the question does nothing",
			item:   "Sync one pack",
			keys:   []string{"enter", "enter"},
			want:   nil,
			screen: screenConfirm,
		},
		{
			name:   "inspecting a pack never syncs",
			item:   "List registered packs",
			keys:   []string{"enter", "y"},
			want:   nil,
			screen: screenDetail,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(t.TempDir())
			m.cursor = menuIndex(t, m, tt.item)
			m.Update(key("enter"))
			m.Update(packsLoadedMsg{packs: packs})
			for _, k := range tt.keys {
				m.Update(key(k))
			}

			switch {
			case tt.want == nil && m.Action != nil:
				t.Fatalf("Action = %+v, want none", *m.Action)
			case tt.want != nil && m.Action == nil:
				t.Fatalf("no Action, want %+v", *tt.want)
			case tt.want != nil && *m.Action != *tt.want:
				t.Fatalf("Action = %+v, want %+v", *m.Action, *tt.want)
			}
			if tt.want == nil && m.screen != tt.screen {
				t.Errorf("screen = %d, want %d", m.screen, tt.screen)
			}
		})
	}
}

func TestConfirmViewNamesThePackAndTheDefault(t *testing.T) {
	m := New(t.TempDir())
	m.selected = &config.PackConfig{Name: "icon-sets-emoji"}
	m.screen = screenConfirm
	view := m.View()
	if !strings.Contains(view, "icon-sets-emoji") || !strings.Contains(view, "[y/N]") {
		t.Fatalf("view = %q", view)
	}
}
