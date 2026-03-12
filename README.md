# tuffman
tuffman is an agent orchestrator

## Building

### With CGO explicitly enabled (required for SQLite)
`CGO_ENABLED=1 go build -o tuffman ./cmd/tuffman`

### Optimized build (smaller binary)
`CGO_ENABLED=1 go build -ldflags="-s -w" -o tuffman ./cmd/tuffman`
