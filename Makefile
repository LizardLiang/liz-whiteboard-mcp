BINARY := liz-whiteboard-mcp

.PHONY: build test test-integration vet run clean

build:
	go build -o $(BINARY) ./cmd/mcp/

# Unit tests (no DB required). Always green.
test:
	go test ./...

# Integration tests — read against the main app's live SQLite database (data/app.db).
# No DB container needed; just point DATABASE_URL at the file.
# Set DATABASE_URL, LIZ_SESSION_TOKEN, and optionally LIZ_SOCKET_URL before running.
#
#   make test-integration \
#     DATABASE_URL=file:/absolute/path/to/liz-whiteboard/data/app.db \
#     LIZ_SESSION_TOKEN=<cookie-value>
test-integration:
	go test -tags=integration -v ./internal/integration/...

vet:
	go vet ./...

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY)
