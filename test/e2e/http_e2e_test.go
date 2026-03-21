// http_e2e_test.go exercises the HTTP phase 1 flows across admin, provider, discovery, config, A2A, and console routes.
package e2e

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	a2ahttp "etcdisc/internal/api/http/a2a"
	adminhttp "etcdisc/internal/api/http/admin"
	confighttp "etcdisc/internal/api/http/config"
	"etcdisc/internal/api/http/console"
	discoveryhttp "etcdisc/internal/api/http/discovery"
	"etcdisc/internal/api/http/middleware"
	registryhttp "etcdisc/internal/api/http/registry"
	"etcdisc/internal/core/model"
	a2asvc "etcdisc/internal/core/service/a2a"
	auditsvc "etcdisc/internal/core/service/audit"
	configsvc "etcdisc/internal/core/service/config"
	discoverysvc "etcdisc/internal/core/service/discovery"
	healthsvc "etcdisc/internal/core/service/health"
	namespacesvc "etcdisc/internal/core/service/namespace"
	registrysvc "etcdisc/internal/core/service/registry"
	"etcdisc/internal/infra/logging"
	"etcdisc/test/testkit"
)

func TestHTTPPhase1E2E(t *testing.T) {
	t.Parallel()

	adminToken := "secret"
	store := testkit.NewMemoryStore()
	clk := namespacesvc.NewFixedClock(time.Now())
	logger := logging.New()
	namespaceService := namespacesvc.NewService(store, clk)
	registryService := registrysvc.NewService(store, namespaceService, healthsvc.NewManager(), clk)
	discoveryService := discoverysvc.NewService(store, registryService)
	configService := configsvc.NewService(store, namespaceService, clk)
	auditService := auditsvc.NewService(store, clk)
	a2aService := a2asvc.NewService(store, namespaceService, registryService, clk)
	mux := http.NewServeMux()
	adminWrap := func(h http.Handler) http.Handler { return middleware.WithAdminToken(adminToken, h) }
	mux.Handle("/admin/v1/namespaces", adminWrap(http.HandlerFunc(adminhttp.NamespaceAPI{Service: namespaceService, Audit: auditService}.HandleCollection)))
	mux.Handle("/admin/v1/config/items", adminWrap(http.HandlerFunc(adminhttp.ConfigAPI{Service: configService, Audit: auditService}.HandleItems)))
	mux.Handle("/console/system", adminWrap(console.SystemPage{Ready: func(context.Context) error { return nil }}))
	mux.Handle("/v1/registry/register", middleware.WithRequestLog(logger, http.HandlerFunc(registryhttp.API{Service: registryService}.Register)))
	mux.Handle("/v1/discovery/instances", middleware.WithRequestLog(logger, http.HandlerFunc(discoveryhttp.API{Service: discoveryService}.Snapshot)))
	mux.Handle("/v1/config/effective", middleware.WithRequestLog(logger, http.HandlerFunc(confighttp.API{Service: configService}.Effective)))
	mux.Handle("/v1/a2a/agentcards", middleware.WithRequestLog(logger, http.HandlerFunc(a2ahttp.API{Service: a2aService}.UpsertCard)))
	mux.Handle("/v1/a2a/discovery", middleware.WithRequestLog(logger, http.HandlerFunc(a2ahttp.API{Service: a2aService}.Discover)))
	server := httptest.NewServer(mux)
	defer server.Close()

	adminCreate := httptest.NewRequest(http.MethodPost, server.URL+"/admin/v1/namespaces", strings.NewReader(`{"name":"prod-core"}`))
	adminCreate.Header.Set("Authorization", "Bearer "+adminToken)
	adminCreateResp := httptest.NewRecorder()
	mux.ServeHTTP(adminCreateResp, adminCreate)
	require.Equal(t, http.StatusCreated, adminCreateResp.Code)

	registerResp, err := http.Post(server.URL+"/v1/registry/register", "application/json", strings.NewReader(`{"instance":{"namespace":"prod-core","service":"payment-api","instanceId":"node-1","address":"127.0.0.1","port":8080}}`))
	require.NoError(t, err)
	_ = registerResp.Body.Close()
	require.Equal(t, http.StatusCreated, registerResp.StatusCode)

	configReq := httptest.NewRequest(http.MethodPost, server.URL+"/admin/v1/config/items", strings.NewReader(`{"item":{"scope":"service","namespace":"prod-core","service":"payment-api","key":"timeout.request","value":"1000","valueType":"duration"}}`))
	configReq.Header.Set("Authorization", "Bearer "+adminToken)
	configResp := httptest.NewRecorder()
	mux.ServeHTTP(configResp, configReq)
	require.Equal(t, http.StatusCreated, configResp.Code)

	_, err = registryService.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "agent-api", AgentID: "agent-1", InstanceID: "inst-1", Address: "10.0.0.9", Port: 9090}})
	require.NoError(t, err)
	cardResp, err := http.Post(server.URL+"/v1/a2a/agentcards", "application/json", strings.NewReader(`{"card":{"namespace":"prod-core","agentId":"agent-1","service":"agent-api","capabilities":["tool.search"],"protocols":["grpc"],"authMode":"static_token"}}`))
	require.NoError(t, err)
	_ = cardResp.Body.Close()
	require.Equal(t, http.StatusCreated, cardResp.StatusCode)

	discoveryResp, err := http.Get(server.URL + "/v1/discovery/instances?namespace=prod-core&service=payment-api")
	require.NoError(t, err)
	body := mustReadAll(t, discoveryResp)
	require.Contains(t, body, "node-1")

	effectiveResp, err := http.Get(server.URL + "/v1/config/effective?namespace=prod-core&service=payment-api")
	require.NoError(t, err)
	body = mustReadAll(t, effectiveResp)
	require.Contains(t, body, "timeout.request")

	a2aResp, err := http.Get(server.URL + "/v1/a2a/discovery?namespace=prod-core&capability=tool.search")
	require.NoError(t, err)
	body = mustReadAll(t, a2aResp)
	require.Contains(t, body, "10.0.0.9")

	consoleReq := httptest.NewRequest(http.MethodGet, server.URL+"/console/system", nil)
	consoleReq.Header.Set("Authorization", "Bearer "+adminToken)
	consoleResp := httptest.NewRecorder()
	mux.ServeHTTP(consoleResp, consoleReq)
	require.Equal(t, http.StatusOK, consoleResp.Code)
	require.Contains(t, consoleResp.Body.String(), "System Status")
}

func mustReadAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(body)
}
