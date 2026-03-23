// config.go defines runtime configuration for the etcdisc phase 1 server and SDK transports.
package config

// Config is the root application configuration.
type Config struct {
	App     AppConfig     `yaml:"app"`
	HTTP    HTTPConfig    `yaml:"http"`
	GRPC    GRPCConfig    `yaml:"grpc"`
	Etcd    EtcdConfig    `yaml:"etcd"`
	Cluster ClusterConfig `yaml:"cluster"`

	Admin AdminConfig `yaml:"admin"`
}

// AppConfig stores generic application metadata.
type AppConfig struct {
	Name string `yaml:"name"`
	Env  string `yaml:"env"`
}

// HTTPConfig stores the HTTP listener configuration.
type HTTPConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// GRPCConfig stores the gRPC listener configuration.
type GRPCConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// EtcdConfig stores etcd client connection settings.
type EtcdConfig struct {
	Endpoints []string `yaml:"endpoints"`
	DialMS    int      `yaml:"dialMS"`
}

// AdminConfig stores the static admin token used by the first version.
type AdminConfig struct {
	Token string `yaml:"token"`
}

// ClusterConfig stores runtime cluster membership and leader election settings.
type ClusterConfig struct {
	Enabled                bool   `yaml:"enabled"`
	NodeID                 string `yaml:"nodeId"`
	AdvertiseHTTPAddr      string `yaml:"advertiseHTTPAddr"`
	AdvertiseGRPCAddr      string `yaml:"advertiseGRPCAddr"`
	MemberTTLSeconds       int    `yaml:"memberTTLSeconds"`
	MemberKeepAliveSeconds int    `yaml:"memberKeepAliveSeconds"`
	LeaderTTLSeconds       int    `yaml:"leaderTTLSeconds"`
	LeaderKeepAliveSeconds int    `yaml:"leaderKeepAliveSeconds"`
}

// Default returns the baseline configuration used when no file or env override is provided.
func Default() Config {
	return Config{
		App: AppConfig{
			Name: "etcdisc",
			Env:  "dev",
		},
		HTTP: HTTPConfig{
			Host: "0.0.0.0",
			Port: 8080,
		},
		GRPC: GRPCConfig{
			Host: "0.0.0.0",
			Port: 9090,
		},
		Etcd: EtcdConfig{
			Endpoints: []string{"127.0.0.1:2379"},
			DialMS:    3000,
		},
		Cluster: ClusterConfig{
			Enabled:                false,
			MemberTTLSeconds:       30,
			MemberKeepAliveSeconds: 10,
			LeaderTTLSeconds:       30,
			LeaderKeepAliveSeconds: 10,
		},
		Admin: AdminConfig{
			Token: "change-me-in-dev",
		},
	}
}
