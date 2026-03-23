// wiring.go builds the runtime dependency graph for the phase 1 HTTP and gRPC servers.
package wiring

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	grpcserver "etcdisc/internal/api/grpcserver"
	a2ahttp "etcdisc/internal/api/http/a2a"
	adminhttp "etcdisc/internal/api/http/admin"
	confighttp "etcdisc/internal/api/http/config"
	"etcdisc/internal/api/http/console"
	discoveryhttp "etcdisc/internal/api/http/discovery"
	"etcdisc/internal/api/http/middleware"
	registryhttp "etcdisc/internal/api/http/registry"
	appconfig "etcdisc/internal/app/config"
	a2asvc "etcdisc/internal/core/service/a2a"
	auditsvc "etcdisc/internal/core/service/audit"
	configsvc "etcdisc/internal/core/service/config"
	discoverysvc "etcdisc/internal/core/service/discovery"
	healthsvc "etcdisc/internal/core/service/health"
	namespacesvc "etcdisc/internal/core/service/namespace"
	registrysvc "etcdisc/internal/core/service/registry"
	"etcdisc/internal/infra/clock"
	"etcdisc/internal/infra/etcd"
	"etcdisc/internal/infra/logging"
	"etcdisc/internal/infra/metrics"
	httpSwagger "github.com/swaggo/http-swagger/v2"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
)

// Dependencies collects runtime objects built during bootstrap.
type Dependencies struct {
	Router     *http.ServeMux
	Logger     *slog.Logger
	GRPCServer *grpc.Server
	ETCDClient *clientv3.Client
	ReadyCheck adminhttp.ReadyCheck
}

// Close releases opened infrastructure clients.
func (d Dependencies) Close() error {
	if d.ETCDClient != nil {
		return d.ETCDClient.Close()
	}
	return nil
}

// Build creates the phase 1 runtime graph.
func Build(cfg appconfig.Config) (Dependencies, error) {
	logger := logging.New()
	etcdClient, err := etcd.NewClient(cfg)
	if err != nil {
		return Dependencies{}, err
	}
	store := etcd.NewStore(etcdClient)
	clk := clock.RealClock{}
	namespaceService := namespacesvc.NewService(store, clk)
	healthManager := healthsvc.NewManager()
	registryService := registrysvc.NewService(store, namespaceService, healthManager, clk)
	discoveryService := discoverysvc.NewService(store, registryService)
	configService := configsvc.NewService(store, namespaceService, clk)
	auditService := auditsvc.NewService(store, clk)
	a2aService := a2asvc.NewService(store, namespaceService, registryService, clk)
	readyCheck := func(ctx context.Context) error { return store.Status(ctx) }

	registryAPI := registryhttp.API{Service: registryService}
	discoveryAPI := discoveryhttp.API{Service: discoveryService}
	configAPI := confighttp.API{Service: configService}
	a2aAPI := a2ahttp.API{Service: a2aService}
	namespaceAPI := adminhttp.NamespaceAPI{Service: namespaceService, Audit: auditService}
	adminConfigAPI := adminhttp.ConfigAPI{Service: configService, Audit: auditService}
	adminInstanceAPI := adminhttp.InstanceAPI{Service: registryService}
	adminA2AAPI := adminhttp.A2AAPI{Service: a2aService}
	adminAuditAPI := adminhttp.AuditAPI{Service: auditService}
	adminSystemAPI := adminhttp.SystemAPI{Ready: readyCheck, Metrics: metrics.Handler()}

	mux := http.NewServeMux()
	mux.Handle("/swagger/", httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json")))
	mux.HandleFunc("/healthz", adminhttp.HealthHandler)
	mux.Handle("/ready", adminhttp.NewReadyHandler(readyCheck))
	mux.Handle("/metrics", metrics.Handler())
	mux.Handle("/v1/registry/register", middleware.WithRequestLog(logger, http.HandlerFunc(registryAPI.Register)))
	mux.Handle("/v1/registry/heartbeat", middleware.WithRequestLog(logger, http.HandlerFunc(registryAPI.Heartbeat)))
	mux.Handle("/v1/registry/update", middleware.WithRequestLog(logger, http.HandlerFunc(registryAPI.Update)))
	mux.Handle("/v1/registry/deregister", middleware.WithRequestLog(logger, http.HandlerFunc(registryAPI.Deregister)))
	mux.Handle("/v1/discovery/instances", middleware.WithRequestLog(logger, http.HandlerFunc(discoveryAPI.Snapshot)))
	mux.Handle("/v1/discovery/watch", middleware.WithRequestLog(logger, http.HandlerFunc(discoveryAPI.Watch)))
	mux.Handle("/v1/config/effective", middleware.WithRequestLog(logger, http.HandlerFunc(configAPI.Effective)))
	mux.Handle("/v1/config/watch", middleware.WithRequestLog(logger, http.HandlerFunc(configAPI.Watch)))
	mux.Handle("/v1/a2a/agentcards", middleware.WithRequestLog(logger, http.HandlerFunc(a2aAPI.UpsertCard)))
	mux.Handle("/v1/a2a/discovery", middleware.WithRequestLog(logger, http.HandlerFunc(a2aAPI.Discover)))

	adminWrap := func(handler http.Handler) http.Handler {
		return middleware.WithRequestLog(logger, middleware.WithAdminToken(cfg.Admin.Token, handler))
	}
	mux.Handle("/admin/v1/namespaces", adminWrap(http.HandlerFunc(namespaceAPI.HandleCollection)))
	mux.Handle("/admin/v1/namespaces/", adminWrap(http.HandlerFunc(namespaceAPI.HandleItem)))
	mux.Handle("/admin/v1/config/items", adminWrap(http.HandlerFunc(adminConfigAPI.HandleItems)))
	mux.Handle("/admin/v1/services/instances", adminWrap(http.HandlerFunc(adminInstanceAPI.HandleList)))
	mux.Handle("/admin/v1/agentcards", adminWrap(http.HandlerFunc(adminA2AAPI.HandleCards)))
	mux.Handle("/admin/v1/audit", adminWrap(http.HandlerFunc(adminAuditAPI.HandleList)))
	mux.Handle("/admin/v1/system", adminWrap(http.HandlerFunc(adminSystemAPI.HandleSummary)))
	mux.Handle("/admin/v1/system/metrics", adminWrap(http.HandlerFunc(adminSystemAPI.HandleMetrics)))
	mux.Handle("/console/namespaces", adminWrap(console.NamespacePage{Service: namespaceService}))
	mux.Handle("/console/services", adminWrap(console.ServiceOverviewPage{Registry: registryService}))
	mux.Handle("/console/policies", adminWrap(console.PolicyPage{Config: configService}))
	mux.Handle("/console/a2a", adminWrap(console.A2APage{A2A: a2aService}))
	mux.Handle("/console/audit", adminWrap(console.AuditPage{Audit: auditService}))
	mux.Handle("/console/system", adminWrap(console.SystemPage{Ready: readyCheck}))

	return Dependencies{
		Router:     mux,
		Logger:     logger,
		GRPCServer: grpcserver.New(grpcserver.Services{Registry: registryService, Discovery: discoveryService, Config: configService, A2A: a2aService}),
		ETCDClient: etcdClient,
		ReadyCheck: readyCheck,
	}, nil
}

func init() {
	_ = os.Setenv("TZ", "UTC")
}
