package main

import (
	"os"

	"also/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
