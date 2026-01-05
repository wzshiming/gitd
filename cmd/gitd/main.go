// Command gitd is a git server that uses the git binary to serve repositories over HTTP.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/wzshiming/gitd"
)

func main() {
	var (
		addr    = flag.String("addr", ":8080", "HTTP server address")
		repoDir = flag.String("repo", ".", "Directory containing git repositories")
		gitPath = flag.String("git", "", "Path to git binary (default: git from PATH)")
	)
	flag.Parse()

	// Verify the repository directory exists
	if info, err := os.Stat(*repoDir); err != nil || !info.IsDir() {
		log.Fatalf("Repository directory does not exist or is not a directory: %s", *repoDir)
	}

	handler := gitd.NewHandler(*repoDir)
	if *gitPath != "" {
		handler.GitBinPath = *gitPath
	}

	log.Printf("Starting git server on %s, serving repositories from %s", *addr, *repoDir)
	if err := http.ListenAndServe(*addr, handler); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting server: %v\n", err)
		os.Exit(1)
	}
}
