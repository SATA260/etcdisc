// client.go builds the etcd client used by storage adapters in the etcdisc server.
package etcd

import (
	"fmt"
	"time"

	appconfig "etcdisc/internal/app/config"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// NewClient creates an etcd client from the application config.
func NewClient(cfg appconfig.Config) (*clientv3.Client, error) {
	if len(cfg.Etcd.Endpoints) == 0 {
		return nil, fmt.Errorf("etcd endpoints are empty")
	}

	return clientv3.New(clientv3.Config{
		Endpoints:   cfg.Etcd.Endpoints,
		DialTimeout: time.Duration(cfg.Etcd.DialMS) * time.Millisecond,
	})
}
