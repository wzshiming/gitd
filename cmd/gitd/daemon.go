package main

import (
	"log"
	"net"
	"path/filepath"
	"strconv"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/osfs"
	gitbackend "github.com/go-git/go-git/v6/backend/git"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/spf13/cobra"

	gitserver "github.com/wzshiming/gitd/server/git"
)

var (
	daemonExportAll bool
	daemonPort      int
	daemonListen    string
)

func init() {
	daemonCmd.Flags().BoolVarP(&daemonExportAll, "export-all", "", false, "Export all repositories without requiring git-daemon-export-ok file")
	daemonCmd.Flags().IntVarP(&daemonPort, "port", "", 9418, "Port to run the Git daemon on")
	daemonCmd.Flags().StringVarP(&daemonListen, "listen", "", "", "Address to listen on (default: all interfaces)")

	rootCmd.AddCommand(daemonCmd)
}

var daemonCmd = &cobra.Command{
	Use:   "daemon [options] [<directory>...]",
	Short: "Start a Git daemon server",
	Long:  `Start a Git daemon that serves repositories over the Git protocol (port 9418).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dirs := args
		if len(dirs) == 0 {
			dirs = append(dirs, ".")
		}

		loader := NewDirsLoader(dirs, false, daemonExportAll)
		addr := net.JoinHostPort(daemonListen, strconv.Itoa(daemonPort))
		be := gitbackend.NewBackend(loader)
		srv := &gitserver.Server{
			Addr:     addr,
			Handler:  gitserver.LoggingMiddleware(log.Default(), be),
			ErrorLog: log.Default(),
		}

		log.Printf("Starting Git daemon on %q", addr)

		return srv.ListenAndServe()
	},
}

type dirsLoader struct {
	loaders   []transport.Loader
	fss       []billy.Filesystem
	exportAll bool
}

var _ transport.Loader = (*dirsLoader)(nil)

// NewDirsLoader creates a new dirsLoader with the given directories.
func NewDirsLoader(dirs []string, strict, exportAll bool) *dirsLoader {
	loaders := make([]transport.Loader, 0, len(dirs))
	fss := make([]billy.Filesystem, 0, len(dirs))

	for _, dir := range dirs {
		abs, err := filepath.Abs(dir)
		if err != nil {
			continue
		}

		fs := osfs.New(abs, osfs.WithBoundOS())
		fss = append(fss, fs)
		loaders = append(loaders, transport.NewFilesystemLoader(fs, strict))
	}

	return &dirsLoader{loaders: loaders, fss: fss, exportAll: exportAll}
}

// Load implements transport.Loader.
func (d *dirsLoader) Load(ep *transport.Endpoint) (storage.Storer, error) {
	for i, loader := range d.loaders {
		storer, err := loader.Load(ep)
		if err == nil {
			if !d.exportAll {
				// We need to check if git-daemon-export-ok
				// file exists and if it does not, we skip this
				// repository.
				dfs := d.fss[i]
				okFile := filepath.Join(ep.Path, "git-daemon-export-ok")

				stat, err := dfs.Lstat(okFile)
				if err != nil || (stat != nil && !stat.Mode().IsRegular()) {
					// If the file does not exist or is a directory,
					// we skip this repository.
					continue
				}
			}

			return storer, nil
		}
	}

	return nil, transport.ErrRepositoryNotFound
}
