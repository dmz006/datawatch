package main

func wire(r *Router, cfg *Config) {
	r.SetConfigureFunc(commConfigureFunc(cfg))
}

func commConfigureFunc(cfg *Config) func(key, value string) error {
	return func(key, value string) error {
		req, err := http.NewRequest(http.MethodPut, loopbackBaseURL(cfg)+"/api/config", nil)
		_ = req
		return err
	}
}

func runConfigSet(cfg *Config, key, value string) error {
	url := loopbackBaseURL(cfg) + "/api/config"
	return putConfig(url, cfg.Server.Token, key+value)
}
