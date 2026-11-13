package workbench

// commandsSort is how the bottom command bar orders its chips. It is a display
// preference, saved with the drawer so the order survives a restart.
type commandsSort uint8

const (
	// commandsSortCreated keeps the snippet list order: the order commands were
	// added, which is the order the Snippets page lists them in.
	commandsSortCreated commandsSort = iota
	// commandsSortName orders by name, case-insensitively.
	commandsSortName
	// commandsSortGroup orders by group name (ungrouped first), then by name.
	commandsSortGroup
)

// settingsCommandsSortChoices is the order the picker offers them in.
var settingsCommandsSortChoices = [...]commandsSort{commandsSortCreated, commandsSortName, commandsSortGroup}

func (sort commandsSort) String() string {
	switch sort {
	case commandsSortName:
		return "name"
	case commandsSortGroup:
		return "group"
	}
	return "created"
}

func commandsSortFromString(value string) commandsSort {
	switch value {
	case "name":
		return commandsSortName
	case "group":
		return commandsSortGroup
	}
	return commandsSortCreated
}

func (state *State) settingsCommandsSortLabel(sort commandsSort) string {
	m := state.messages.Tools
	switch sort {
	case commandsSortName:
		return m.CommandsSortName
	case commandsSortGroup:
		return m.CommandsSortGroup
	}
	return m.CommandsSortCreated
}
