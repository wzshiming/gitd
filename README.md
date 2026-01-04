# gitd

A native Go implementation of a Git server supporting both HTTP and Git daemon protocols.

## Features

- **HTTP Server**: Serve Git repositories over HTTP protocol
- **Git Daemon**: Serve Git repositories over the native Git protocol (port 9418)
- **Pure Go**: No external dependencies on native Git binary for server operation
- Built on top of [go-git](https://github.com/go-git/go-git)

## Installation

```bash
go install github.com/wzshiming/gitd/cmd/gitd@latest
```

## Usage

### HTTP Server

Start an HTTP server to serve Git repositories:

```bash
# Serve repositories from the current directory on port 8080
gitd http .

# Serve from a specific directory on a custom port
gitd http -p 3000 /path/to/repos
```

Clients can then clone repositories:

```bash
git clone http://localhost:8080/myrepo.git
```

### Git Daemon

Start a Git daemon server (native git:// protocol):

```bash
# Serve repositories from the current directory on port 9418
gitd daemon

# Serve all repositories (without requiring git-daemon-export-ok)
gitd daemon --export-all /path/to/repos

# Listen on a specific address and port
gitd daemon --listen 0.0.0.0 --port 9418 /path/to/repos
```

By default, repositories need to have a `git-daemon-export-ok` file in the repository root to be served. Use `--export-all` to skip this check.

Clients can then clone repositories:

```bash
git clone git://localhost:9418/myrepo.git
```

## Command Reference

### gitd http

```
Start an HTTP server that serves Git repositories over HTTP.

Usage:
  gitd http [options] <directory> [flags]

Flags:
  -h, --help            help for http
  -p, --port int        Port to run the HTTP server on (default 8080)
      --prefix string   Prefix for the HTTP server routes
```

### gitd daemon

```
Start a Git daemon that serves repositories over the Git protocol (port 9418).

Usage:
  gitd daemon [options] [<directory>...] [flags]

Flags:
      --export-all      Export all repositories without requiring git-daemon-export-ok file
  -h, --help            help for daemon
      --listen string   Address to listen on (default: all interfaces)
      --port int        Port to run the Git daemon on (default 9418)
```

## License

MIT License - see [LICENSE](LICENSE) for details.
