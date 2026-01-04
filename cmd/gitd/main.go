package main

import (
	"log"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "gitd",
	Short: "A native Go implementation of a Git server",
	Long:  `gitd is a Git server implemented in pure Go, supporting both HTTP and Git daemon protocols.`,
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
