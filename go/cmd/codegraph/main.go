// Command codegraph is the CLI entry point for the CodeGraph Go binary.
package main

import (
	"fmt"
	"os"
)

// Version is the current version of the CodeGraph binary.
// It is injected at build time via -ldflags="-X main.Version=<tag>".
var Version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Printf("codegraph %s\n", Version)
		os.Exit(0)
	}

	fmt.Printf("codegraph %s\n", Version)
	fmt.Println("Run 'codegraph --help' for usage.")
}
