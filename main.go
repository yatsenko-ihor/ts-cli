package main

import (
	"fmt"
	"os"

	"github.com/ihor/ts-cli/commands"
	"github.com/mitchellh/cli"
)

const version = "0.1.0"

func main() {
	c := cli.NewCLI("ts-cli", version)
	c.Args = os.Args[1:]

	c.Commands = map[string]cli.CommandFactory{
		"login": func() (cli.Command, error) {
			return &commands.LoginCommand{}, nil
		},
		"list": func() (cli.Command, error) {
			return &commands.ListCommand{}, nil
		},
	}

	exitStatus, err := c.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
	}

	os.Exit(exitStatus)
}
