// bootstrap.go wires config, infrastructure, and HTTP routes, then starts the etcdisc server.
package bootstrap

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	appconfig "etcdisc/internal/app/config"
	"etcdisc/internal/app/wiring"
)

// Run boots the server with a default local configuration.
func Run() error {
	cfg, err := appconfig.Load()
	if err != nil {
		return err
	}

	deps, err := wiring.Build(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = deps.Close() }()

	httpSrv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.HTTP.Host, cfg.HTTP.Port),
		Handler:           deps.Router,
		ReadHeaderTimeout: 5 * time.Second,
	}
	grpcListener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.GRPC.Host, cfg.GRPC.Port))
	if err != nil {
		return err
	}

	deps.Logger.InfoContext(context.Background(), "starting etcdisc http server", "addr", httpSrv.Addr)
	deps.Logger.InfoContext(context.Background(), "starting etcdisc grpc server", "addr", grpcListener.Addr().String())
	errCh := make(chan error, 2)
	go func() { errCh <- httpSrv.ListenAndServe() }()
	go func() { errCh <- deps.GRPCServer.Serve(grpcListener) }()
	return <-errCh
}
