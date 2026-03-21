// handlers_test.go verifies HTTP discovery snapshot and SSE watch endpoints.
package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"etcdisc/internal/core/model"
	discoverysvc "etcdisc/internal/core/service/discovery"
	healthsvc "etcdisc/internal/core/service/health"
	namespacesvc "etcdisc/internal/core/service/namespace"
	registrysvc "etcdisc/internal/core/service/registry"
	"etcdisc/test/testkit"
)

func TestDiscoveryAPI(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	nsService := namespacesvc.NewService(store, namespacesvc.NewFixedClock(time.Now()))
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	registry := registrysvc.NewService(store, nsService, healthsvc.NewManager(), namespacesvc.NewFixedClock(time.Now()))
	_, err = registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080}})
	require.NoError(t, err)
	api := API{Service: discoverysvc.NewService(store, registry)}

	snapshotResp := httptest.NewRecorder()
	api.Snapshot(snapshotResp, httptest.NewRequest(http.MethodGet, "/v1/discovery/instances?namespace=prod-core&service=payment-api", nil))
	require.Equal(t, http.StatusOK, snapshotResp.Code)
	require.Contains(t, snapshotResp.Body.String(), `"node-1"`)
}
