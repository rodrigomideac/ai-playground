package main

import (
	"fmt"
	"os"

	"github.com/rodrigomideac/ai-playground/internal/ui"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		if _, silent := err.(*silentError); !silent {
			fmt.Fprintf(os.Stderr, "%s %s\n", ui.Red("✗"), err)
		}
		os.Exit(1)
	}
}
