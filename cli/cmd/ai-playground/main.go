package main

import (
	"fmt"
	"os"

	"github.com/rodrigomideac/ai-playground/internal/ui"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%s %s\n", ui.Red("✗"), err)
		os.Exit(1)
	}
}
