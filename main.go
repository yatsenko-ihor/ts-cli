package main

import (
	"os"

	"github.com/ihor/ts-cli/commands"
)

func main() {
	rootCmd := commands.NewRootCommand()
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
