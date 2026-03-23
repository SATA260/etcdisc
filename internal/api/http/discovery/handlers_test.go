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

type plainWriter struct{ header http.Header }

func (w *plainWriter) Header() http.Header       { return w.header }
func (w *plainWriter) Write([]byte) (int, error) { return 0, nil }
func (w *plainWriter) WriteHeader(int)           {}

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

	methodResp := httptest.NewRecorder()
	api.Snapshot(methodResp, httptest.NewRequest(http.MethodPost, "/v1/discovery/instances", nil))
	require.Equal(t, http.StatusMethodNotAllowed, methodResp.Code)

	watchReq := httptest.NewRequest(http.MethodGet, "/v1/discovery/watch?namespace=prod-core&service=payment-api", nil)
	watchCtx, cancel := context.WithCancel(watchReq.Context())
	watchReq = watchReq.WithContext(watchCtx)
	watchResp := httptest.NewRecorder()
	go api.Watch(watchResp, watchReq)
	time.Sleep(20 * time.Millisecond)
	_, err = registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-2", Address: "127.0.0.2", Port: 8080}})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return watchResp.Body.Len() > 0 }, 2*time.Second, 20*time.Millisecond)
	require.Contains(t, watchResp.Body.String(), "event: put")
	cancel()

	metadataResp := httptest.NewRecorder()
	api.Snapshot(metadataResp, httptest.NewRequest(http.MethodGet, "/v1/discovery/instances?namespace=prod-core&service=payment-api&meta.zone=a&healthyOnly=false", nil))
	require.Equal(t, http.StatusOK, metadataResp.Code)

	watchMethodResp := httptest.NewRecorder()
	api.Watch(watchMethodResp, httptest.NewRequest(http.MethodPost, "/v1/discovery/watch", nil))
	require.Equal(t, http.StatusMethodNotAllowed, watchMethodResp.Code)

	plain := &plainWriter{header: http.Header{}}
	api.Watch(plain, httptest.NewRequest(http.MethodGet, "/v1/discovery/watch?namespace=prod-core&service=payment-api", nil))
}

func TestDiscoveryHelpers(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/?healthyOnly=true&meta.zone=a&meta.color=blue", nil)
	value, ok := parseBoolQuery(req, "healthyOnly")
	require.True(t, ok)
	require.True(t, value)
	_, ok = parseBoolQuery(httptest.NewRequest(http.MethodGet, "/?healthyOnly=not-bool", nil), "healthyOnly")
	require.False(t, ok)
	require.Equal(t, map[string]string{"zone": "a", "color": "blue"}, parseMetadataFilters(req))
	require.Nil(t, parseMetadataFilters(httptest.NewRequest(http.MethodGet, "/", nil)))
}
