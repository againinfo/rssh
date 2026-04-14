package commands

import (
	"fmt"
	"io"

	"rssh/internal/server/users"
	"rssh/internal/terminal"
)

type who struct {
}

func (w *who) ValidArgs() map[string]string {
	return map[string]string{}
}

func (w *who) Run(user *users.User, tty io.ReadWriter, line terminal.ParsedLine) error {

	allUsers := users.ListUsers()

	for _, user := range allUsers {
		fmt.Fprintf(tty, "%s\n", user)
	}

	return nil
}

func (w *who) Expect(line terminal.ParsedLine) []string {
	return nil
}

func (w *who) Help(explain bool) string {
	const description = "List users connected to the RSSH server"
	if explain {
		return description
	}

	return terminal.MakeHelpText(w.ValidArgs(),
		"who",
		description)
}
