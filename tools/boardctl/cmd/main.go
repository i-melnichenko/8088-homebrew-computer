package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/apps"
)

func main() {
	if err := apps.Run(os.Args[1:]); err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintf(os.Stderr, "boardctl: %v\n", err)
		os.Exit(1)
	}
}
