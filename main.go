package main

import (
	"os"

	"github.com/seinsdieum/also/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
