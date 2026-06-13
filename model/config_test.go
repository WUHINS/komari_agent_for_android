package model

import (
	"encoding/json"
	"testing"
)

func TestJsonUnmarshalConfig(t *testing.T) {
	var conf AgentConfig
	conf.Debug = true
	t.Logf("old conf: %+v", conf)
	var newConfJson = `{"gpu": true}`
	if err := json.Unmarshal([]byte(newConfJson), &conf); err != nil {
		t.Errorf("json unmarshal failed: %v", err)
	}
	t.Logf("new conf: %+v", conf)
	if conf.GPU != true {
		t.Errorf("json unmarshal failed: %v", conf.GPU)
	}
	if conf.Debug != true {
		t.Errorf("json unmarshal failed: %v", conf.Debug)
	}
}

func TestParseArgs(t *testing.T) {
	args := []string{"agent", "--endpoint=https://example.com", "--token=abc123", "--gpu", "--interval=2.0"}
	cfg, err := ParseArgs(args)
	if err != nil {
		t.Fatalf("ParseArgs failed: %v", err)
	}
	if cfg.Endpoint != "https://example.com" {
		t.Errorf("expected endpoint https://example.com, got %s", cfg.Endpoint)
	}
	if cfg.Token != "abc123" {
		t.Errorf("expected token abc123, got %s", cfg.Token)
	}
	if !cfg.GPU {
		t.Errorf("expected GPU=true")
	}
	if cfg.Interval != 2.0 {
		t.Errorf("expected interval 2.0, got %f", cfg.Interval)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Interval != 1.0 {
		t.Errorf("expected default interval 1.0, got %f", cfg.Interval)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("expected default max_retries 3, got %d", cfg.MaxRetries)
	}
}
