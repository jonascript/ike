package tui

import "charm.land/bubbles/v2/key"

type keyMap struct {
	Quadrant key.Binding
	NextQuad key.Binding
	PrevQuad key.Binding
	Up       key.Binding
	Down     key.Binding
	MoveUp   key.Binding
	MoveDown key.Binding
	Add      key.Binding
	Edit     key.Binding
	Title    key.Binding
	Done     key.Binding
	Move     key.Binding
	Delete   key.Binding
	Undo     key.Binding
	Redo     key.Binding
	Archive  key.Binding
	Restore  key.Binding
	Help     key.Binding
	Quit     key.Binding
	Confirm  key.Binding
	Cancel   key.Binding

	// Space keys. NewSpace and RenameSpace are live only inside the picker, so
	// `n` and `r` stay free on the matrix and `r` does not collide with the
	// archive view's restore.
	Spaces      key.Binding
	NextSpace   key.Binding
	PrevSpace   key.Binding
	NewSpace    key.Binding
	RenameSpace key.Binding

	// File keys. OpenFile is live only inside the file picker.
	Files    key.Binding
	OpenFile key.Binding

	// Delegation keys. Plan opens the attached plan, DraftPlan asks an agent
	// for one, and Agent hands the task over — or reattaches to a live run.
	// They are capitals so the lowercase letters stay free, and so neither sits
	// next to `d` for delete.
	Plan      key.Binding
	DraftPlan key.Binding
	Agent     key.Binding
}

var keys = keyMap{
	Quadrant: key.NewBinding(key.WithKeys("1", "2", "3", "4"), key.WithHelp("1-4", "focus quadrant")),
	NextQuad: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next quadrant")),
	PrevQuad: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev quadrant")),
	Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	MoveUp:   key.NewBinding(key.WithKeys("shift+up", "K"), key.WithHelp("K", "move task up")),
	MoveDown: key.NewBinding(key.WithKeys("shift+down", "J"), key.WithHelp("J", "move task down")),
	Add:      key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
	Edit:     key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
	Title:    key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "rename quadrant")),
	Done:     key.NewBinding(key.WithKeys("x", "enter"), key.WithHelp("x", "done")),
	Move:     key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "move")),
	Delete:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	Undo:     key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "undo")),
	Redo:     key.NewBinding(key.WithKeys("U", "ctrl+r"), key.WithHelp("U", "redo")),
	Archive:  key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "archive")),
	Restore:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "restore")),
	Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Confirm:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
	Cancel:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),

	Spaces:      key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "spaces")),
	NextSpace:   key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "next space")),
	PrevSpace:   key.NewBinding(key.WithKeys("["), key.WithHelp("[", "prev space")),
	NewSpace:    key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new space")),
	RenameSpace: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rename space")),

	Files:    key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "files")),
	OpenFile: key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open a path")),

	Plan:      key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "plan")),
	DraftPlan: key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "draft a plan")),
	Agent:     key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delegate")),
}
