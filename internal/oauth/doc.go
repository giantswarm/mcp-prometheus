// Package oauth provides OAuth 2.1 Authorization Server setup for mcp-prometheus.
//
// It wraps the [github.com/giantswarm/mcp-oauth] library to initialise an HTTP handler
// that exposes the full OAuth 2.1 flow (PKCE, token rotation, dynamic client
// registration) backed by a Dex OIDC provider.
//
// # Usage
//
//	handler, cleanup, err := oauth.NewHandler(ctx, cfg, logger)
//	if err != nil { ... }
//	defer cleanup()
//	handler.RegisterAuthorizationServerMetadataRoutes(mux)
//	handler.RegisterProtectedResourceMetadataRoutes(mux, "/mcp")
//	mux.HandleFunc("/oauth/authorize", handler.ServeAuthorization)
//	mux.Handle("/mcp", handler.ValidateToken(mcpHandler))
package oauth
