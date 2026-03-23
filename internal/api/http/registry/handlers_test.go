// handlers_test.go verifies HTTP registry endpoints for register, update, heartbeat, and deregister.
package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	healthsvc "etcdisc/internal/core/service/health"
	namespacesvc "etcdisc/internal/core/service/namespace"
	registrysvc "etcdisc/internal/core/service/registry"
	"etcdisc/test/testkit"
)

func TestRegistryAPI(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	nsService := namespacesvc.NewService(store, namespacesvc.NewFixedClock(time.Now()))
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	service := registrysvc.NewService(store, nsService, healthsvc.NewManager(), namespacesvc.NewFixedClock(time.Now()))
	api := API{Service: service}

	registerResp := httptest.NewRecorder()
	api.Register(registerResp, httptest.NewRequest(http.MethodPost, "/v1/registry/register", strings.NewReader(`{"instance":{"namespace":"prod-core","service":"payment-api","instanceId":"node-1","address":"127.0.0.1","port":8080}}`)))
	require.Equal(t, http.StatusCreated, registerResp.Code)
	registered, err := service.Get(context.Background(), "prod-core", "payment-api", "node-1")
	require.NoError(t, err)

	updateResp := httptest.NewRecorder()
	api.Update(updateResp, httptest.NewRequest(http.MethodPost, "/v1/registry/update", strings.NewReader(`{"namespace":"prod-core","service":"payment-api","instanceId":"node-1","expectedRevision":`+strconv.FormatInt(registered.Revision, 10)+`,"weight":200}`)))
	require.Equal(t, http.StatusOK, updateResp.Code)

	heartbeatResp := httptest.NewRecorder()
	api.Heartbeat(heartbeatResp, httptest.NewRequest(http.MethodPost, "/v1/registry/heartbeat", strings.NewReader(`{"namespace":"prod-core","service":"payment-api","instanceId":"node-1"}`)))
	require.Equal(t, http.StatusOK, heartbeatResp.Code)

	deregisterResp := httptest.NewRecorder()
	api.Deregister(deregisterResp, httptest.NewRequest(http.MethodPost, "/v1/registry/deregister", strings.NewReader(`{"namespace":"prod-core","service":"payment-api","instanceId":"node-1"}`)))
	require.Equal(t, http.StatusOK, deregisterResp.Code)

	methodResp := httptest.NewRecorder()
	api.Register(methodResp, httptest.NewRequest(http.MethodGet, "/v1/registry/register", nil))
	require.Equal(t, http.StatusMethodNotAllowed, methodResp.Code)

	badRegisterResp := httptest.NewRecorder()
	api.Register(badRegisterResp, httptest.NewRequest(http.MethodPost, "/v1/registry/register", strings.NewReader(`{"instance":`)))
	require.Equal(t, http.StatusBadRequest, badRegisterResp.Code)

	badJSONResp := httptest.NewRecorder()
	api.Update(badJSONResp, httptest.NewRequest(http.MethodPost, "/v1/registry/update", strings.NewReader(`{"namespace":`)))
	require.Equal(t, http.StatusBadRequest, badJSONResp.Code)

	heartbeatMethodResp := httptest.NewRecorder()
	api.Heartbeat(heartbeatMethodResp, httptest.NewRequest(http.MethodGet, "/v1/registry/heartbeat", nil))
	require.Equal(t, http.StatusMethodNotAllowed, heartbeatMethodResp.Code)

	badHeartbeatResp := httptest.NewRecorder()
	api.Heartbeat(badHeartbeatResp, httptest.NewRequest(http.MethodPost, "/v1/registry/heartbeat", strings.NewReader(`{"namespace":`)))
	require.Equal(t, http.StatusBadRequest, badHeartbeatResp.Code)

	updateMethodResp := httptest.NewRecorder()
	api.Update(updateMethodResp, httptest.NewRequest(http.MethodGet, "/v1/registry/update", nil))
	require.Equal(t, http.StatusMethodNotAllowed, updateMethodResp.Code)

	deregisterMethodResp := httptest.NewRecorder()
	api.Deregister(deregisterMethodResp, httptest.NewRequest(http.MethodGet, "/v1/registry/deregister", nil))
	require.Equal(t, http.StatusMethodNotAllowed, deregisterMethodResp.Code)

	badDeregisterResp := httptest.NewRecorder()
	api.Deregister(badDeregisterResp, httptest.NewRequest(http.MethodPost, "/v1/registry/deregister", strings.NewReader(`{"namespace":`)))
	require.Equal(t, http.StatusBadRequest, badDeregisterResp.Code)
}
