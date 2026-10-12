package main

import (
	"os"

	"lintel/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:], cli.Option{}))
}
