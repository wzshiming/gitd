# gitd

A Git server that uses the git binary to serve repositories over HTTP.

## Features

- Git protocol endpoints for clone, fetch, push
- Git LFS support
- Repository management API
- Repository import/mirroring
- **Lazy mirror and cache support** (inspired by [Google Goblet](https://github.com/google/goblet))

## Lazy Mirror

The lazy mirror feature enables on-demand mirroring of Git repositories. When a client requests a repository that doesn't exist locally, gitd can automatically create a mirror of an upstream repository and start fetching content in the background. This is useful for creating pull-through caches.

### Usage

```go
package main

import (
	"log"
	"net/http"
	"strings"

	"github.com/wzshiming/gitd"
)

func main() {
	// Define a function that maps repository names to upstream URLs
	lazyMirrorFunc := func(repoName string) string {
		// Example: mirror GitHub repositories
		if strings.HasPrefix(repoName, "github.com/") {
			return "https://" + repoName
		}
		return "" // Return empty to disable mirroring for this repo
	}

	handler := gitd.NewHandler(
		gitd.WithRootDir("./data"),
		gitd.WithLazyMirrorSource(lazyMirrorFunc),
	)

	log.Fatal(http.ListenAndServe(":8080", handler))
}
```

With lazy mirror enabled, when a user runs:
```bash
git clone http://localhost:8080/github.com/user/repo.git
```

The server will:
1. Create a local mirror repository
2. Configure it to fetch from the upstream URL
3. Start importing content in the background
4. Serve the content to the client as it becomes available

## API Endpoints

### Repository Management

- `GET /api/repositories` - List all repositories
- `GET /api/repositories/{repo}.git` - Get repository info
- `POST /api/repositories/{repo}.git` - Create repository
- `DELETE /api/repositories/{repo}.git` - Delete repository

### Import/Mirror

- `POST /api/repositories/{repo}.git/import` - Import from source URL
- `GET /api/repositories/{repo}.git/import/status` - Get import status
- `POST /api/repositories/{repo}.git/sync` - Sync mirror with upstream
- `GET /api/repositories/{repo}.git/mirror` - Get mirror configuration

## Running

```bash
go run ./cmd/gitd -addr :8080 -repo ./data
```
