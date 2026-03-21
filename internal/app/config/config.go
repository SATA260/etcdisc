// config.go defines runtime configuration for the etcdisc phase 1 server and SDK transports.
package config

// Config is the root application configuration.
type Config struct {
	App  AppConfig  `yaml:"app"`
	HTTP HTTPConfig `yaml:"http"`
	GRPC GRPCConfig `yaml:"grpc"`
	Etcd EtcdConfig `yaml:"etcd"`

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
		Admin: AdminConfig{
			Token: "change-me-in-dev",
		},
	}
}
