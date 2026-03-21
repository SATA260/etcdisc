// handlers_test.go verifies HTTP AgentCard registration and capability discovery endpoints.
package a2a

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
	healthsvc "etcdisc/internal/core/service/health"
	namespacesvc "etcdisc/internal/core/service/namespace"
	registrysvc "etcdisc/internal/core/service/registry"
	"etcdisc/test/testkit"
)

func TestA2AAPI(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	nsService := namespacesvc.NewService(store, namespacesvc.NewFixedClock(time.Now()))
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	registry := registrysvc.NewService(store, nsService, healthsvc.NewManager(), namespacesvc.NewFixedClock(time.Now()))
	_, err = registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "agent-api", AgentID: "agent-1", InstanceID: "inst-1", Address: "10.0.0.9", Port: 9090}})
	require.NoError(t, err)
	api := API{Service: a2asvc.NewService(store, nsService, registry, namespacesvc.NewFixedClock(time.Now()))}

	upsertResp := httptest.NewRecorder()
	api.UpsertCard(upsertResp, httptest.NewRequest(http.MethodPost, "/v1/a2a/agentcards", strings.NewReader(`{"card":{"namespace":"prod-core","agentId":"agent-1","service":"agent-api","capabilities":["tool.search"],"protocols":["grpc"],"authMode":"static_token"}}`)))
	require.Equal(t, http.StatusCreated, upsertResp.Code)

	discoverResp := httptest.NewRecorder()
	api.Discover(discoverResp, httptest.NewRequest(http.MethodGet, "/v1/a2a/discovery?namespace=prod-core&capability=tool.search&protocol=grpc", nil))
	require.Equal(t, http.StatusOK, discoverResp.Code)
	require.Contains(t, discoverResp.Body.String(), "10.0.0.9")
}
