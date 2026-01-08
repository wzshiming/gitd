# gitd

A Git HTTP server with support for Git LFS (Large File Storage).

## Features

- Git HTTP server supporting standard Git operations (clone, push, pull)
- Git LFS support for efficient large file handling
- File locking support for LFS
- RESTful API for repository management

## Dependencies

This project uses the following key dependencies:

- [git-lfs](https://github.com/git-lfs/git-lfs) - Git Large File Storage support

## Testing

The test suite requires `git` and `git-lfs` binaries to be installed on your system.

To run tests:

```bash
go test ./...
```

## Usage

```bash
go run ./cmd/gitd
```

## License

See LICENSE file for details.
