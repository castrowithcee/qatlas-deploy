# Test entry points; every test recipe runs through scripts/test-run, which applies the host test budget.
set positional-arguments

run := "scripts/test-run"

# Downloads the Go modules and builds the e2e base images; the only recipe that needs the internet.
deps:
    @go mod download
    @e2e/run.sh deps

# Formatting, vet and the Go tests; stops at the first failure. Optional package filter.
test-fast *args:
    @if [ $# -eq 0 ]; then set -- ./...; fi; {{run}} sh -ec 'f=$(gofmt -l .); [ -z "$f" ] || { echo "gofmt: $f" >&2; exit 1; }; go vet "$@"; go test -count=1 -failfast "$@"' sh "$@"

# Formatting, vet and all Go tests. Optional package filter.
test *args:
    @if [ $# -eq 0 ]; then set -- ./...; fi; {{run}} sh -ec 'f=$(gofmt -l .); [ -z "$f" ] || { echo "gofmt: $f" >&2; exit 1; }; go vet "$@"; go test -count=1 "$@"' sh "$@"

# The e2e scenario on a fresh simulated host, without the internet.
test-e2e:
    @TEST_KIND=e2e {{run}} e2e/run.sh check scenario

# The acceptance scenario with real third-party images; copies them from the internet.
test-ext:
    @TEST_KIND=e2e TEST_NET=1 {{run}} e2e/run.sh check acceptance

# Rewrites the golden files of the terminal interface; review the diff like code.
snap-update:
    @{{run}} go test -count=1 ./internal/tui/ -update
