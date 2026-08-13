build:
    go build -buildvcs=false -o /tmp/synk ./cmd/synk

test:
    go test ./cmd/... ./internal/...

vet:
    go vet ./cmd/... ./internal/...

check:
    go test ./cmd/... ./internal/...
    go vet ./cmd/... ./internal/...
    bash -n scripts/release/*.sh
    nix flake check path:.
