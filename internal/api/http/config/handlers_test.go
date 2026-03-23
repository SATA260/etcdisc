// handlers_test.go verifies HTTP config effective reads and watch endpoints.
package config

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"etcdisc/internal/core/model"
	configsvc "etcdisc/internal/core/service/config"
	namespacesvc "etcdisc/internal/core/service/namespace"
	"etcdisc/test/testkit"
)

type plainWriter struct{ header http.Header }

func (w *plainWriter) Header() http.Header       { return w.header }
func (w *plainWriter) Write([]byte) (int, error) { return 0, nil }
func (w *plainWriter) WriteHeader(int)           {}

func TestConfigAPI(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	nsService := namespacesvc.NewService(store, namespacesvc.NewFixedClock(time.Now()))
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	service := configsvc.NewService(store, nsService, namespacesvc.NewFixedClock(time.Now()))
	item, err := service.Put(context.Background(), configsvc.PutInput{Item: model.ConfigItem{Scope: model.ConfigScopeService, Namespace: "prod-core", Service: "payment-api", Key: "timeout.request", Value: "1000", ValueType: model.ConfigValueDuration}})
	require.NoError(t, err)
	api := API{Service: service}

	effectiveResp := httptest.NewRecorder()
	api.Effective(effectiveResp, httptest.NewRequest(http.MethodGet, "/v1/config/effective?namespace=prod-core&service=payment-api", nil))
	require.Equal(t, http.StatusOK, effectiveResp.Code)
	require.Contains(t, effectiveResp.Body.String(), "effectiveConfig")
	require.Contains(t, effectiveResp.Body.String(), "timeout.request")

	methodResp := httptest.NewRecorder()
	api.Effective(methodResp, httptest.NewRequest(http.MethodPost, "/v1/config/effective", nil))
	require.Equal(t, http.StatusMethodNotAllowed, methodResp.Code)

	watchReq := httptest.NewRequest(http.MethodGet, "/v1/config/watch?namespace=prod-core&service=payment-api", nil)
	watchCtx, cancel := context.WithCancel(watchReq.Context())
	watchReq = watchReq.WithContext(watchCtx)
	watchResp := httptest.NewRecorder()
	go api.Watch(watchResp, watchReq)
	time.Sleep(20 * time.Millisecond)
	_, err = service.Put(context.Background(), configsvc.PutInput{Item: model.ConfigItem{Scope: model.ConfigScopeService, Namespace: "prod-core", Service: "payment-api", Key: "timeout.request", Value: "1200", ValueType: model.ConfigValueDuration}, ExpectedRevision: item.Revision})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return watchResp.Body.Len() > 0 }, 2*time.Second, 20*time.Millisecond)
	require.Contains(t, watchResp.Body.String(), "event: put")
	cancel()

	watchMethodResp := httptest.NewRecorder()
	api.Watch(watchMethodResp, httptest.NewRequest(http.MethodPost, "/v1/config/watch", nil))
	require.Equal(t, http.StatusMethodNotAllowed, watchMethodResp.Code)

	plain := &plainWriter{header: http.Header{}}
	api.Watch(plain, httptest.NewRequest(http.MethodGet, "/v1/config/watch?namespace=prod-core&service=payment-api", nil))
}
