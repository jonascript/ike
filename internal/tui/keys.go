package tui

import "charm.land/bubbles/v2/key"

type keyMap struct {
	Quadrant key.Binding
	NextQuad key.Binding
	PrevQuad key.Binding
	Up       key.Binding
	Down     key.Binding
	Add      key.Binding
	Edit     key.Binding
	Done     key.Binding
	Move     key.Binding
	Delete   key.Binding
	Archive  key.Binding
	Help     key.Binding
	Quit     key.Binding
	Confirm  key.Binding
	Cancel   key.Binding
}

var keys = keyMap{
	Quadrant: key.NewBinding(key.WithKeys("1", "2", "3", "4"), key.WithHelp("1-4", "focus quadrant")),
	NextQuad: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next quadrant")),
	PrevQuad: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev quadrant")),
	Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Add:      key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
	Edit:     key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
	Done:     key.NewBinding(key.WithKeys("x", "enter"), key.WithHelp("x", "done")),
	Move:     key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "move")),
	Delete:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	Archive:  key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "archive")),
	Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Confirm:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
	Cancel:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}
