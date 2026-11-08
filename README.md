# pruneplex

## Building

Requires only Go (see `go.mod`). The server is pure Go with embedded assets, so
cross-compiling needs no C toolchain.

```sh
go run ./tools/build                         # all platforms -> dist/
go run ./tools/build -target host            # just this machine
go run ./tools/build -target linux/arm64,windows/amd64
go run ./tools/build -version v1.0.0 -out release
go run ./tools/build -list                   # default targets
```

Output is `dist/pruneplex-<os>-<arch>[.exe]` plus `dist/SHA256SUMS`. The same
commands work on Windows, macOS and Linux.

## Running

```sh
PORT=8081 ./dist/pruneplex-linux-amd64
```

## Tests

```sh
go test ./...
npm test
```

## Releases

CI (`.github/workflows/ci.yml`) runs tests on every push/PR and builds all
platforms. Pushing a tag like `v1.0.0` publishes a GitHub release with the
binaries and `SHA256SUMS`:

```sh
git tag v1.0.0 && git push origin v1.0.0
```
