package config

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	ControllerURL  string
	Token          string
	AgentID        string
	SourceMode     string
	DBPath         string
	APIBaseURL     string
	APIToken       string
	AllowWrite     bool
	LogTraffic     bool
	PingInterval   time.Duration
	ReconnectDelay time.Duration
	RequestTimeout time.Duration
}

type jsonConfig struct {
	Controller      string `json:"controller"`
	Token           string `json:"token"`
	AgentId         string `json:"agentId"`
	Source          string `json:"source"`
	DbPath          string `json:"dbPath"`
	ApiBaseUrl      string `json:"apiBaseUrl"`
	ApiToken        string `json:"apiToken"`
	AllowWrite      bool   `json:"allowWrite"`
	LogTraffic      bool   `json:"logTraffic"`
	PingInterval    string `json:"pingInterval"`
	ReconnectDelay  string `json:"reconnectDelay"`
	RequestTimeout  string `json:"requestTimeout"`
}

func FromFlags() Config {
	cfg := Config{
		PingInterval:   20 * time.Second,
		ReconnectDelay: 5 * time.Second,
		RequestTimeout: 10 * time.Second,
	}

	// Try to load from agent-config.json in the same directory as the executable
	exePath, err := os.Executable()
	if err == nil {
		configPath := filepath.Join(filepath.Dir(exePath), "agent-config.json")
		if data, err := os.ReadFile(configPath); err == nil {
			var jcfg jsonConfig
			if err := json.Unmarshal(data, &jcfg); err == nil {
				cfg.ControllerURL = jcfg.Controller
				cfg.Token = jcfg.Token
				cfg.AgentID = jcfg.AgentId
				cfg.SourceMode = jcfg.Source
				cfg.DBPath = jcfg.DbPath
				cfg.APIBaseURL = jcfg.ApiBaseUrl
				cfg.APIToken = jcfg.ApiToken
				cfg.AllowWrite = jcfg.AllowWrite
				cfg.LogTraffic = jcfg.LogTraffic
				if jcfg.PingInterval != "" {
					if d, err := time.ParseDuration(jcfg.PingInterval); err == nil {
						cfg.PingInterval = d
					}
				}
				if jcfg.ReconnectDelay != "" {
					if d, err := time.ParseDuration(jcfg.ReconnectDelay); err == nil {
						cfg.ReconnectDelay = d
					}
				}
				if jcfg.RequestTimeout != "" {
					if d, err := time.ParseDuration(jcfg.RequestTimeout); err == nil {
						cfg.RequestTimeout = d
					}
				}
			}
		}
	}

	// Command-line flags override config file
	flag.StringVar(&cfg.ControllerURL, "controller", getEnv("EKIBEN_CONTROLLER", cfg.ControllerURL), "controller websocket url")
	flag.StringVar(&cfg.Token, "token", getEnv("EKIBEN_TOKEN", cfg.Token), "agent auth token")
	flag.StringVar(&cfg.AgentID, "agent-id", getEnv("EKIBEN_AGENT_ID", cfg.AgentID), "agent id")
	flag.StringVar(&cfg.SourceMode, "source", getEnv("EKIBEN_SOURCE", cfg.SourceMode), "data source mode: direct or api")
	flag.StringVar(&cfg.DBPath, "db", getEnv("EKIBEN_DB", cfg.DBPath), "path to taiko.db3")
	flag.StringVar(&cfg.APIBaseURL, "api-base-url", getEnv("EKIBEN_API_BASE_URL", cfg.APIBaseURL), "base url for TLS REST API")
	flag.StringVar(&cfg.APIToken, "api-token", getEnv("EKIBEN_API_TOKEN", cfg.APIToken), "bearer token for TLS REST API (optional)")
	flag.BoolVar(&cfg.AllowWrite, "allow-write", getEnvBool("EKIBEN_ALLOW_WRITE", cfg.AllowWrite), "allow write queries")
	flag.BoolVar(&cfg.LogTraffic, "log-traffic", getEnvBool("EKIBEN_LOG_TRAFFIC", cfg.LogTraffic), "log websocket traffic")
	flag.DurationVar(&cfg.PingInterval, "ping", getEnvDuration("EKIBEN_PING", cfg.PingInterval), "ping interval")
	flag.DurationVar(&cfg.ReconnectDelay, "reconnect", getEnvDuration("EKIBEN_RECONNECT", cfg.ReconnectDelay), "reconnect delay")
	flag.DurationVar(&cfg.RequestTimeout, "timeout", getEnvDuration("EKIBEN_TIMEOUT", cfg.RequestTimeout), "request timeout")

	flag.Parse()
	return cfg
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if v == "1" || v == "true" || v == "TRUE" || v == "yes" || v == "YES" {
			return true
		}
		return false
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
