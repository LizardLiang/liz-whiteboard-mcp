// Package integration contains live integration tests that require a real
// PostgreSQL database and (optionally) a running Socket.IO server.
//
// Tests are guarded by the "integration" build tag and skip cleanly when
// DATABASE_URL is not set, so default `go test ./...` always passes.
//
// # Running integration tests
//
//	DATABASE_URL=postgres://... \
//	LIZ_SESSION_TOKEN=<cookie-value> \
//	LIZ_SOCKET_URL=ws://localhost:3010 \
//	go test -tags=integration -v ./internal/integration/...
//
// # Optional per-test env vars
//
//	INTG_TEST_USER_ID       — UUID of a real user in the DB (for project list test)
//	INTG_TEST_PROJECT_ID    — UUID of a real project (for whiteboard list test)
//	INTG_TEST_WHITEBOARD_ID — UUID of a real whiteboard (for bulk position test)
//
// Tests that require these vars skip automatically when unset.
//
// # Local database with Docker
//
//	docker-compose up -d postgres
//	# then run your main-app migrations against postgres://localhost:5432/...
//	DATABASE_URL=postgres://postgres:postgres@localhost:5432/lizwhiteboard \
//	go test -tags=integration -v ./internal/integration/...
package integration
