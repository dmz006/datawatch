package config

type ServerConfig struct {
	Port   int          `yaml:"port"`
	TLS    bool         `yaml:"tls,omitempty"`
	Limits LimitsConfig `yaml:"limits"`
}

type LimitsConfig struct {
	Max int `yaml:"max"`
}

type PtrConfig struct {
	Flag bool `yaml:"flag"`
}

type Shared struct {
	SharedOne string `yaml:"shared_one"`
}

type Embedded struct {
	EmbeddedOne string `yaml:"embedded_one"`
}
