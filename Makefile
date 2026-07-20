# reolink-onvif-shim — makes Mom's Reolink doorbell adoptable by UniFi Protect.
#
# Common use:
#   make run      # build and run against config.json (no root needed — high ports)
#   make logs     # tail the log in another window
#   make test     # run the test suite

BIN     := reolink-onvif-shim
CONFIG  := config.json
LOG     := reolink-onvif-shim.log

.DEFAULT_GOAL := build

.PHONY: build
build: ## Build the native binary
	go build -o $(BIN) .

.PHONY: run
run: build ## Build and run in the foreground (logs to terminal + $(LOG))
	./$(BIN) -config $(CONFIG) -log $(LOG)

.PHONY: logs
logs: ## Tail the log file
	tail -f $(LOG)

.PHONY: test
test: ## Run all tests
	go test ./...

.PHONY: race
race: ## Run tests with the race detector
	go test -race ./...

.PHONY: vet
vet: ## go vet + gofmt check
	go vet ./...
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed on:"; gofmt -l .; exit 1)

.PHONY: check
check: vet test ## vet + test

.PHONY: universal
universal: ## Build a universal (Intel + Apple Silicon) macOS binary in dist/
	@mkdir -p dist
	GOOS=darwin GOARCH=amd64 go build -o dist/$(BIN)-amd64 .
	GOOS=darwin GOARCH=arm64 go build -o dist/$(BIN)-arm64 .
	lipo -create -output dist/$(BIN) dist/$(BIN)-amd64 dist/$(BIN)-arm64
	@rm -f dist/$(BIN)-amd64 dist/$(BIN)-arm64
	@echo "built dist/$(BIN):"; lipo -info dist/$(BIN)

.PHONY: mem
mem: ## Show the most recent memory-stats lines from the log
	@grep MEMSTATS $(LOG) | tail -20

.PHONY: clean
clean: ## Remove build artifacts
	rm -f $(BIN)
	rm -rf dist

.PHONY: help
help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
