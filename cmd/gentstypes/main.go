package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/raterudder/raterudder/pkg/apigen"
)

func main() {
	root := flag.String("root", ".", "path to repository root")
	flag.Parse()

	if err := apigen.WriteOutput(*root); err != nil {
		fmt.Fprintf(os.Stderr, "gentstypes: %v\n", err)
		os.Exit(1)
	}
}
