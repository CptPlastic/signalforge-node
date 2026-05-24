package main

import (
	"fmt"
	"os"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
