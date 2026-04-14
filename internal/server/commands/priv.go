package commands

import (
	"fmt"
	"io"

	"rssh/internal/server/users"
	"rssh/internal/terminal"
)

type privilege struct {
}

func (p *privilege) ValidArgs() map[string]string {
	return map[string]string{}
}

func (p *privilege) Run(user *users.User, tty io.ReadWriter, line terminal.ParsedLine) error {

	fmt.Fprintf(tty, "%s\n", user.PrivilegeString())

	return nil
}

func (p *privilege) Expect(line terminal.ParsedLine) []string {
	return nil
}

func (p *privilege) Help(explain bool) string {
	if explain {
		return "Privilege shows the current user privilege level."
	}

	return terminal.MakeHelpText(p.ValidArgs(),
		"priv ",
		"Print the currrent user privilege level.",
	)
}
