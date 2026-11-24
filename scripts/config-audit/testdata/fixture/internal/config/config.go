package config

import "time"

// Config is the fixture configuration struct. One key is missing from each
// audited surface: no_ref (reference), no_web (Settings), no_get (GET), no_put (PUT).
type Config struct {
	Hostname string `yaml:"hostname"`
	NoRef    string `yaml:"no_ref"`
	NoWeb    bool   `yaml:"no_web"`
	NoGet    int    `yaml:"no_get"`
	NoPut    int    `yaml:"no_put"`
	Secret   string `yaml:"-"`
	Untagged string
	Server   ServerConfig `yaml:"server"`
	Ptr      *PtrConfig   `yaml:"ptr"`
	Shared   `yaml:",inline"`
	Embedded                   // untagged: a field named after the type, as yaml.v3 does
	Labels   map[string]string `yaml:"labels"`
	List     []string          `yaml:"list"`
	Wait     time.Duration     `yaml:"wait"`
}
