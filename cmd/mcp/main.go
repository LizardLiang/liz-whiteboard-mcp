// Command mcp is the entrypoint for the liz-whiteboard MCP server.
//
// Phase 3 (JWKS JWT verifier):
//   - Default verifier: NewJWKSVerifier — validates RS256 JWTs from the self-hosted AS.
//   - Dev-only verifier: NewStubVerifier — only active when MCP_DEV_AUTH=stub.
//   - Serves GET /.well-known/oauth-protected-resource (RFC 9728 PRM).
//   - Wraps POST /mcp with bearer-token middleware (auth.RequireBearerToken).
//   - Identity is per-request from the validated token (auth.UserID(ctx)).
//     LIZ_SESSION_TOKEN is no longer required or consulted.
//
// Required env (non-stub mode):
//   DATABASE_URL                SQLite path (e.g. file:/path/to/db.sqlite)
//   OAUTH_ISSUER                AS issuer URL (e.g. http://localhost:3000)
//                               MUST match the iss claim in every access token
//                               and the OAUTH_ISSUER env var on the AS side.
//   MCP_RESOURCE_URI            Canonical MCP resource URI (e.g. http://localhost:3011/mcp)
//                               MUST match the aud claim in every access token
//                               and the MCP_RESOURCE_URI env var on the AS side.
//
// Optional env:
//   LIZ_SOCKET_URL              WebSocket URL for the collaboration server.
//   MCP_LISTEN_ADDR             Listen address (default 127.0.0.1:3011).
//
// Dev-only env (never set in production):
//   MCP_DEV_AUTH=stub           Activates the stub verifier (skips JWKS).
//   MCP_DEV_STUB_TOKEN          The one token the stub will accept.
//   MCP_DEV_USER_ID             User ID the stub injects (default "dev-user").
//
// ENV ALIGNMENT (important — mismatched values cause 401 on every request):
//   The AS and RS MUST share identical values for:
//     OAUTH_ISSUER      → AS: issuer of tokens  / RS: expected iss claim
//     MCP_RESOURCE_URI  → AS: aud in minted tokens / RS: expected aud claim
//   Local dev defaults: OAUTH_ISSUER=http://localhost:3000, MCP_RESOURCE_URI=http://localhost:3011/mcp
//   The AS's default MCP_RESOURCE_URI is http://localhost:8080/mcp — set it to
//   http://localhost:3011/mcp to match the RS default (or override both consistently).
//
// Go rewrite of src/mcp/index.ts. Produces a single compiled binary with no
// Bun or Node dependency.
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/auth"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/db"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/socket"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/tools"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Establish the database connection pool (read path + auth queries).
	if _, err := db.Connect(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "[liz-whiteboard MCP] Fatal error: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// Determine whether to use the dev stub or the production JWKS verifier.
	//
	// The stub is ONLY activated when MCP_DEV_AUTH=stub is explicitly set.
	// In all other cases the JWKS verifier is used and OAUTH_ISSUER /
	// MCP_RESOURCE_URI MUST be present — we fail fast if they are not.
	devStubMode := os.Getenv("MCP_DEV_AUTH") == "stub"

	// Canonical URI for this resource server.
	// Used in the PRM Resource field, the WWW-Authenticate header, and (in JWKS
	// mode) as the expected aud claim in every access token.
	resourceURI := os.Getenv("MCP_RESOURCE_URI")

	// Issuer URL of the Authorization Server (the liz-whiteboard app).
	// Advertised in PRM authorization_servers and (in JWKS mode) used to build the
	// JWKS URL and validate the iss claim in every access token.
	oauthIssuer := os.Getenv("OAUTH_ISSUER")

	if !devStubMode {
		// Production path: both variables are required. Without them we cannot
		// build a correctly-configured verifier — boot with a clear error rather
		// than silently accepting or rejecting every token.
		if oauthIssuer == "" {
			fmt.Fprintln(os.Stderr,
				"[liz-whiteboard MCP] Fatal: OAUTH_ISSUER must be set (e.g. http://localhost:3000).\n"+
					"  This must match the issuer URL configured on the Authorization Server.\n"+
					"  To use the dev stub instead, set MCP_DEV_AUTH=stub.")
			os.Exit(1)
		}
		if resourceURI == "" {
			fmt.Fprintln(os.Stderr,
				"[liz-whiteboard MCP] Fatal: MCP_RESOURCE_URI must be set (e.g. http://localhost:3011/mcp).\n"+
					"  This must match the aud claim the Authorization Server puts in access tokens.\n"+
					"  On the AS side set MCP_RESOURCE_URI to the same value.\n"+
					"  To use the dev stub instead, set MCP_DEV_AUTH=stub.")
			os.Exit(1)
		}
	} else {
		// Dev stub: provide friendly defaults so the PRM response is useful.
		if oauthIssuer == "" {
			oauthIssuer = "http://localhost:3000"
		}
		if resourceURI == "" {
			resourceURI = "http://localhost:3011/mcp"
		}
	}

	// Per RFC 9728 §4, the well-known path is /.well-known/oauth-protected-resource.
	// Served at the server root regardless of the resource URI path.
	const prmPath = "/.well-known/oauth-protected-resource"

	// Protected Resource Metadata (RFC 9728).
	prm := &oauthex.ProtectedResourceMetadata{
		Resource:                resourceURI,
		AuthorizationServers:    []string{oauthIssuer},
		ScopesSupported:         []string{"whiteboard"},
		ResourceName:            "liz-whiteboard MCP",
		BearerMethodsSupported:  []string{"header"},
	}

	// Select verifier: stub (dev only) or JWKS (production default).
	var verifier sdkauth.TokenVerifier
	if devStubMode {
		verifier = auth.NewStubVerifier()
	} else {
		// OAUTH_JWKS_URL lets the RS fetch JWKS from an internal address while
		// OAUTH_ISSUER stays the public value validated in the iss claim — needed
		// when the AS and RS sit behind one public domain via a reverse proxy.
		// Defaults to {issuer}/.well-known/jwks.json when unset.
		jwksURL := os.Getenv("OAUTH_JWKS_URL")
		verifier = auth.NewJWKSVerifier(oauthIssuer, resourceURI, jwksURL)
	}

	// ResourceMetadataURL (advertised in WWW-Authenticate) is set once below,
	// after listenAddr is resolved.
	bearerOpts := &sdkauth.RequireBearerTokenOptions{
		Scopes: []string{"whiteboard"},
	}

	// Create the MCP server.
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "liz-whiteboard",
		Version: "1.0.0",
	}, nil)

	// Register all 17 tools.
	tools.RegisterDiscoveryTools(server)    // list_projects, list_whiteboards
	tools.RegisterReadTools(server)         // get_board, get_schema_summary
	tools.RegisterTableTools(server)        // create_table, update_table, delete_table
	tools.RegisterColumnTools(server)       // create_column, update_column, delete_column, reorder_columns
	tools.RegisterRelationshipTools(server) // create_relationship, update_relationship, delete_relationship
	tools.RegisterPositionsTools(server)    // bulk_update_positions
	tools.RegisterStaticTools(server)       // list_data_types, list_cardinalities

	// Build the Streamable HTTP handler, then wrap with bearer middleware.
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, nil)
	protectedMCPHandler := sdkauth.RequireBearerToken(verifier, bearerOpts)(mcpHandler)

	mux := http.NewServeMux()
	// RFC 9728 Protected Resource Metadata — public, no auth.
	mux.Handle(prmPath, sdkauth.ProtectedResourceMetadataHandler(prm))
	// MCP endpoint — bearer-protected.
	mux.Handle("/mcp", protectedMCPHandler)

	// Configurable listen address; defaults to localhost-only (no TLS in Phase 2).
	// TLS is a Phase 5 hardening item per the oauth-remote-mcp tactical plan.
	listenAddr := os.Getenv("MCP_LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = "127.0.0.1:3011"
	}

	// Advertise the PUBLIC Protected Resource Metadata URL in WWW-Authenticate.
	// Behind a reverse proxy the listen address is internal, so derive the public
	// origin from MCP_RESOURCE_URI (e.g. http://localhost:8080/mcp → origin
	// http://localhost:8080). Falls back to the listen address when unparseable.
	bearerOpts.ResourceMetadataURL = "http://" + listenAddr + prmPath
	if u, err := url.Parse(resourceURI); err == nil && u.Scheme != "" && u.Host != "" {
		bearerOpts.ResourceMetadataURL = u.Scheme + "://" + u.Host + prmPath
	}

	httpServer := &http.Server{
		Addr:         listenAddr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM: close sockets, drain HTTP (so in-flight
	// handlers can finish), then close DB. DB must close last — handlers call db.Pool().
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprint(os.Stderr, "[liz-whiteboard MCP] Shutting down...\n")
		socket.CloseAll()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = httpServer.Shutdown(shutdownCtx)
		db.Close()
		cancel()
	}()

	verifierMode := "JWKS (RS256, production)"
	if devStubMode {
		verifierMode = "DEV STUB — MCP_DEV_AUTH=stub (never use in production)"
	}
	fmt.Fprintf(os.Stderr,
		"[liz-whiteboard MCP] Server ready.\n"+
			"  MCP endpoint:  http://%s/mcp  (bearer-protected)\n"+
			"  Resource meta: http://%s%s\n"+
			"  Resource URI:  %s\n"+
			"  AS issuer:     %s\n"+
			"  Verifier:      %s\n",
		listenAddr, listenAddr, prmPath, resourceURI, oauthIssuer, verifierMode)

	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "[liz-whiteboard MCP] Fatal error: %v\n", err)
		socket.CloseAll()
		db.Close()
		os.Exit(1)
	}
}
