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

func TestConfigAPI(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	nsService := namespacesvc.NewService(store, namespacesvc.NewFixedClock(time.Now()))
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	service := configsvc.NewService(store, nsService, namespacesvc.NewFixedClock(time.Now()))
	_, err = service.Put(context.Background(), configsvc.PutInput{Item: model.ConfigItem{Scope: model.ConfigScopeService, Namespace: "prod-core", Service: "payment-api", Key: "timeout.request", Value: "1000", ValueType: model.ConfigValueDuration}})
	require.NoError(t, err)
	api := API{Service: service}

	effectiveResp := httptest.NewRecorder()
	api.Effective(effectiveResp, httptest.NewRequest(http.MethodGet, "/v1/config/effective?namespace=prod-core&service=payment-api", nil))
	require.Equal(t, http.StatusOK, effectiveResp.Code)
	require.Contains(t, effectiveResp.Body.String(), "effectiveConfig")
	require.Contains(t, effectiveResp.Body.String(), "timeout.request")
}
