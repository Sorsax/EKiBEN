package config

import (
	"flag"
	"os"
	"time"
)

type Config struct {
	ControllerURL  string
	Token          string
	AgentID        string
	DBPath         string
	AllowWrite     bool
	LogTraffic     bool
	PingInterval   time.Duration
	ReconnectDelay time.Duration
	RequestTimeout time.Duration
}

func FromFlags() Config {
	cfg := Config{}

	flag.StringVar(&cfg.ControllerURL, "controller", getEnv("EKIBEN_CONTROLLER", ""), "controller websocket url")
	flag.StringVar(&cfg.Token, "token", getEnv("EKIBEN_TOKEN", ""), "agent auth token")
	flag.StringVar(&cfg.AgentID, "agent-id", getEnv("EKIBEN_AGENT_ID", ""), "agent id")
	flag.StringVar(&cfg.DBPath, "db", getEnv("EKIBEN_DB", ""), "path to taiko.db3")
	flag.BoolVar(&cfg.AllowWrite, "allow-write", getEnvBool("EKIBEN_ALLOW_WRITE", false), "allow write queries")
	flag.BoolVar(&cfg.LogTraffic, "log-traffic", getEnvBool("EKIBEN_LOG_TRAFFIC", false), "log websocket traffic")
	flag.DurationVar(&cfg.PingInterval, "ping", getEnvDuration("EKIBEN_PING", 20*time.Second), "ping interval")
	flag.DurationVar(&cfg.ReconnectDelay, "reconnect", getEnvDuration("EKIBEN_RECONNECT", 5*time.Second), "reconnect delay")
	flag.DurationVar(&cfg.RequestTimeout, "timeout", getEnvDuration("EKIBEN_TIMEOUT", 10*time.Second), "request timeout")

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
