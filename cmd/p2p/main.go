package main

import (
	"os"

	"p2p-file-sharing/pkg/cli"
)

func main() {
	exitCode := cli.Run(os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(exitCode)
}
