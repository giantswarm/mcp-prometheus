package prometheus

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/giantswarm/mcp-oauth/handler"
	"github.com/giantswarm/mcp-oauth/providers"
	"github.com/giantswarm/mcp-oauth/providers/oidc"

	"github.com/giantswarm/mcp-prometheus/internal/server"
)

type staticResolver struct {
	tenants []string
}

func (r *staticResolver) TenantsForGroups(_ context.Context, _ []string) ([]string, error) {
	return r.tenants, nil
}

func newTenancyServerContext(t *testing.T, logBuffer *bytes.Buffer) *server.ServerContext {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(logBuffer, nil))
	sc, err := server.NewServerContext(t.Context(),
		server.WithSlogLogger(logger),
		server.WithOAuthEnabled(true),
		server.WithTenancyResolver(&staticResolver{tenants: []string{"giantswarm"}}),
	)
	if err != nil {
		t.Fatalf("NewServerContext: %v", err)
	}
	return sc
}

func TestResolveTenantOrgIDLogsDelegatedRequest(t *testing.T) {
	var logBuffer bytes.Buffer
	sc := newTenancyServerContext(t, &logBuffer)

	userInfo := &providers.UserInfo{
		ID:           "quentin@example.com",
		Issuer:       "https://muster.example.com",
		Groups:       []string{"customer:giantswarm"},
		TokenSource:  providers.TokenSourceTrustedIssuer,
		ActorSubject: "system:serviceaccount:kagent:sre-agent",
		ActorIssuer:  "https://muster.example.com",
		ActorChain: []oidc.ActorClaim{
			{Issuer: "https://muster.example.com", Subject: "system:serviceaccount:kagent:sre-agent"},
			{Issuer: "https://muster.example.com", Subject: "system:serviceaccount:kagent:planner-agent"},
		},
	}
	ctx := handler.ContextWithUserInfo(t.Context(), userInfo)

	orgID, err := resolveTenantOrgID(ctx, sc, "")
	if err != nil {
		t.Fatalf("resolveTenantOrgID: %v", err)
	}
	if orgID != "giantswarm" {
		t.Errorf("orgID = %q, want %q", orgID, "giantswarm")
	}

	logged := logBuffer.String()
	for _, want := range []string{
		"Delegated request",
		"subject=quentin@example.com",
		"actor_subject=system:serviceaccount:kagent:sre-agent",
		"system:serviceaccount:kagent:planner-agent",
	} {
		if !bytes.Contains([]byte(logged), []byte(want)) {
			t.Errorf("log output missing %q; got: %s", want, logged)
		}
	}
}

func TestResolveTenantOrgIDNoDelegationLogForPlainToken(t *testing.T) {
	var logBuffer bytes.Buffer
	sc := newTenancyServerContext(t, &logBuffer)

	// A muster-issued human login token carries no act claim and must pass
	// without a delegation log line.
	userInfo := &providers.UserInfo{
		ID:          "quentin@example.com",
		Issuer:      "https://muster.example.com",
		Groups:      []string{"customer:giantswarm"},
		TokenSource: providers.TokenSourceTrustedIssuer,
	}
	ctx := handler.ContextWithUserInfo(t.Context(), userInfo)

	orgID, err := resolveTenantOrgID(ctx, sc, "")
	if err != nil {
		t.Fatalf("resolveTenantOrgID: %v", err)
	}
	if orgID != "giantswarm" {
		t.Errorf("orgID = %q, want %q", orgID, "giantswarm")
	}
	if bytes.Contains(logBuffer.Bytes(), []byte("Delegated request")) {
		t.Errorf("unexpected delegation log for a token without act: %s", logBuffer.String())
	}
}

func TestActorChainSubjects(t *testing.T) {
	userInfo := &providers.UserInfo{
		ActorChain: []oidc.ActorClaim{
			{Subject: "outer"},
			{Subject: "inner"},
		},
	}
	subjects := actorChainSubjects(userInfo)
	if len(subjects) != 2 || subjects[0] != "outer" || subjects[1] != "inner" {
		t.Errorf("actorChainSubjects = %v, want [outer inner]", subjects)
	}
}
