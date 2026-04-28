// Command codegraph is the CLI entry point for the CodeGraph Go binary.
package main

import (
	"fmt"
	"os"

	"github.com/kristofer/codegraph/internal/mcp"
)

// Version is the current version of the CodeGraph binary.
// It is injected at build time via -ldflags="-X main.Version=<tag>".
var Version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "version":
			fmt.Printf("codegraph %s\n", Version)
			os.Exit(0)
		case "serve":
			if len(os.Args) > 2 && os.Args[2] == "--mcp" {
				srv := mcp.NewServer("")
				if err := srv.Start(); err != nil {
					fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
					os.Exit(1)
				}
				return
			}
		}
	}

	fmt.Printf("codegraph %s\n", Version)
	fmt.Println("Run 'codegraph --help' for usage.")
}
