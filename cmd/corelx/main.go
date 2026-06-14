package main

import (
	"fmt"
	"os"
	"path/filepath"

	"nitro-core-dx/internal/corelx"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <project: .ncdx | folder | main.corelx> <output.cart>\n", os.Args[0])
		os.Exit(1)
	}
	inputPath := os.Args[1]
	outputPath := os.Args[2]

	// CompileProject resolves .ncdx containers and project folders, loads
	// external image (.cxasset) assets, runs the orphan check, and writes the
	// ROM to OutputPath.
	_, err := corelx.CompileProject(inputPath, &corelx.CompileOptions{OutputPath: outputPath})
	if err != nil {
		if de, ok := err.(*corelx.DiagnosticsError); ok {
			for _, d := range de.Diagnostics {
				if d.Severity == corelx.SeverityError {
					fmt.Fprintf(os.Stderr, "error: %s\n", d.Message)
				}
			}
		} else {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		os.Exit(1)
	}
	fmt.Printf("Compiled %s -> %s\n", filepath.Base(inputPath), filepath.Base(outputPath))
}
