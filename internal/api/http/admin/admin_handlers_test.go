// admin_handlers_test.go verifies core admin APIs for config, instances, A2A, audit, and system summary.
package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"etcdisc/internal/core/model"
	a2asvc "etcdisc/internal/core/service/a2a"
	auditsvc "etcdisc/internal/core/service/audit"
	configsvc "etcdisc/internal/core/service/config"
	healthsvc "etcdisc/internal/core/service/health"
	namespacesvc "etcdisc/internal/core/service/namespace"
	registrysvc "etcdisc/internal/core/service/registry"
	"etcdisc/test/testkit"
)

func TestAdminHandlers(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	clk := namespacesvc.NewFixedClock(time.Now())
	nsService := namespacesvc.NewService(store, clk)
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	registry := registrysvc.NewService(store, nsService, healthsvc.NewManager(), clk)
	configService := configsvc.NewService(store, nsService, clk)
	auditService := auditsvc.NewService(store, clk)
	a2aService := a2asvc.NewService(store, nsService, registry, clk)

	_, err = registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080}})
	require.NoError(t, err)
	_, err = registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "agent-api", AgentID: "agent-1", InstanceID: "inst-1", Address: "10.0.0.9", Port: 9090}})
	require.NoError(t, err)
	_, err = a2aService.Put(context.Background(), a2asvc.PutInput{Card: model.AgentCard{Namespace: "prod-core", AgentID: "agent-1", Service: "agent-api", Capabilities: []string{"tool.search"}, Protocols: []string{"grpc"}, AuthMode: model.AuthModeStaticToken}})
	require.NoError(t, err)
	_, err = auditService.Record(context.Background(), model.AuditEntry{Actor: "admin", Action: "test", Resource: "audit", Message: "seed"})
	require.NoError(t, err)

	configResp := httptest.NewRecorder()
	ConfigAPI{Service: configService, Audit: auditService}.HandleItems(configResp, httptest.NewRequest(http.MethodPost, "/admin/v1/config/items", strings.NewReader(`{"item":{"scope":"service","namespace":"prod-core","service":"payment-api","key":"timeout.request","value":"1000","valueType":"duration"}}`)))
	require.Equal(t, http.StatusCreated, configResp.Code)

	instancesResp := httptest.NewRecorder()
	InstanceAPI{Service: registry}.HandleList(instancesResp, httptest.NewRequest(http.MethodGet, "/admin/v1/services/instances?namespace=prod-core", nil))
	require.Equal(t, http.StatusOK, instancesResp.Code)
	require.Contains(t, instancesResp.Body.String(), "payment-api")

	a2aResp := httptest.NewRecorder()
	A2AAPI{Service: a2aService}.HandleCards(a2aResp, httptest.NewRequest(http.MethodGet, "/admin/v1/agentcards?namespace=prod-core&capability=tool.search", nil))
	require.Equal(t, http.StatusOK, a2aResp.Code)
	require.Contains(t, a2aResp.Body.String(), "agent-1")

	auditResp := httptest.NewRecorder()
	AuditAPI{Service: auditService}.HandleList(auditResp, httptest.NewRequest(http.MethodGet, "/admin/v1/audit", nil))
	require.Equal(t, http.StatusOK, auditResp.Code)
	require.Contains(t, auditResp.Body.String(), "seed")

	systemResp := httptest.NewRecorder()
	SystemAPI{Ready: func(context.Context) error { return nil }, Metrics: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("metrics")) })}.HandleSummary(systemResp, httptest.NewRequest(http.MethodGet, "/admin/v1/system", nil))
	require.Equal(t, http.StatusOK, systemResp.Code)
	require.Contains(t, systemResp.Body.String(), `"ready":true`)

	metricsResp := httptest.NewRecorder()
	SystemAPI{Metrics: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("metrics")) })}.HandleMetrics(metricsResp, httptest.NewRequest(http.MethodGet, "/admin/v1/system/metrics", nil))
	require.Equal(t, http.StatusOK, metricsResp.Code)
	require.Contains(t, metricsResp.Body.String(), "metrics")
}
