# Multi-stage build for the liz-whiteboard MCP Resource Server.
# Pure-Go (modernc.org/sqlite, no cgo) → static binary on distroless.
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mcp ./cmd/mcp/

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/mcp /usr/local/bin/mcp
EXPOSE 3011
ENTRYPOINT ["/usr/local/bin/mcp"]
