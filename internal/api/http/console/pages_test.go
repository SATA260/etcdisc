// pages_test.go verifies service, policy, A2A, audit, and system console pages render key data.
package console

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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

func TestConsolePages(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	clk := namespacesvc.NewFixedClock(time.Now())
	nsService := namespacesvc.NewService(store, clk)
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	registry := registrysvc.NewService(store, nsService, healthsvc.NewManager(), clk)
	configService := configsvc.NewService(store, nsService, clk)
	a2aService := a2asvc.NewService(store, nsService, registry, clk)
	auditService := auditsvc.NewService(store, clk)

	_, err = registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080}})
	require.NoError(t, err)
	_, err = registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "agent-api", AgentID: "agent-1", InstanceID: "inst-1", Address: "10.0.0.9", Port: 9090}})
	require.NoError(t, err)
	_, err = configService.Put(context.Background(), configsvc.PutInput{Item: model.ConfigItem{Scope: model.ConfigScopeService, Namespace: "prod-core", Service: "payment-api", Key: "timeout.request", Value: "1000", ValueType: model.ConfigValueDuration}})
	require.NoError(t, err)
	_, err = a2aService.Put(context.Background(), a2asvc.PutInput{Card: model.AgentCard{Namespace: "prod-core", AgentID: "agent-1", Service: "agent-api", Capabilities: []string{"tool.search"}, Protocols: []string{"grpc"}, AuthMode: model.AuthModeStaticToken}})
	require.NoError(t, err)
	_, err = auditService.Record(context.Background(), model.AuditEntry{Actor: "admin", Action: "seed", Resource: "test", Message: "seeded"})
	require.NoError(t, err)

	assertPage := func(handler http.Handler, path string, contains ...string) {
		t.Helper()
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, resp.Code)
		for _, fragment := range contains {
			require.Contains(t, resp.Body.String(), fragment)
		}
	}

	assertPage(ServiceOverviewPage{Registry: registry}, "/console/services?namespace=prod-core", "Service Overview", "payment-api", "node-1")
	assertPage(PolicyPage{Config: configService}, "/console/policies?namespace=prod-core&service=payment-api&scope=service", "Policy Config", "timeout.request")
	assertPage(A2APage{A2A: a2aService}, "/console/a2a?namespace=prod-core", "A2A AgentCards", "tool.search")
	assertPage(AuditPage{Audit: auditService}, "/console/audit", "Audit Log", "seeded")
	assertPage(SystemPage{Ready: func(context.Context) error { return errors.New("down") }}, "/console/system", "System Status", "false")
}
