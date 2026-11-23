package workbench

// commandsSort is how the command bar orders its groups and the chips in them.
// Ungrouped commands always close the list. It is a display preference, saved
// with the drawer so the order survives a restart.
type commandsSort uint8

const (
	// commandsSortCreated keeps the snippet list order: groups appear where their
	// first command sits and commands keep the order the Snippets page lists them
	// in.
	commandsSortCreated commandsSort = iota
	// commandsSortName orders groups and the commands in them by name,
	// case-insensitively.
	commandsSortName
	// commandsSortGroup orders the groups by name and leaves the commands in each
	// in the list order.
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
