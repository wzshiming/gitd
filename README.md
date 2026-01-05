# gitd

A lightweight HTTP git server written in Go that uses the system git binary to serve repositories.

## Features

- HTTP Smart Protocol support for git operations
- Serves multiple repositories from a directory
- Uses the system git binary for all operations
- Supports both fetch/clone (`git-upload-pack`) and push (`git-receive-pack`)
- Simple and minimal implementation

## Installation

```bash
go install github.com/wzshiming/gitd/cmd/gitd@latest
```

Or build from source:

```bash
git clone https://github.com/wzshiming/gitd.git
cd gitd
go build -o gitd ./cmd/gitd
```

## Usage

### Command Line

```bash
# Serve repositories from the current directory on port 8080
gitd

# Serve repositories from a specific directory
gitd -repo /path/to/repos

# Use a different port
gitd -addr :9000

# Specify a custom git binary path
gitd -git /usr/local/bin/git
```

### As a Library

```go
package main

import (
    "log"
    "net/http"

    "github.com/wzshiming/gitd"
)

func main() {
    handler := gitd.NewHandler("/path/to/repos")
    log.Fatal(http.ListenAndServe(":8080", handler))
}
```

## Git Client Usage

Once the server is running, you can use git commands to interact with repositories:

```bash
# Clone a repository
git clone http://localhost:8080/myrepo.git

# Push changes
cd myrepo
# ... make changes ...
git push origin main

# Fetch updates
git fetch origin
```

## Creating Bare Repositories

To serve repositories, you need to create bare git repositories:

```bash
# Create a new bare repository
git init --bare /path/to/repos/myrepo.git

# Or clone an existing repository as bare
git clone --bare https://github.com/user/repo.git /path/to/repos/repo.git
```

## Configuration Options

| Option | Default | Description |
|--------|---------|-------------|
| `-addr` | `:8080` | HTTP server address |
| `-repo` | `.` | Directory containing git repositories |
| `-git` | `git` | Path to git binary |

## Security Considerations

- This server provides no authentication. All repositories are publicly accessible.
- For production use, consider placing behind a reverse proxy with authentication.
- Path traversal attacks are prevented by validating repository paths.

## License

MIT License - see [LICENSE](LICENSE) file.
