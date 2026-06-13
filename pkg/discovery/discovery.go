package discovery

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type AutoDiscoveryConfig struct {
	UUID  string `json:"uuid"`
	Token string `json:"token"`
}

func ConfigPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), "auto-discovery.json"), nil
}

func Load() (*AutoDiscoveryConfig, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cfg AutoDiscoveryConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Token == "" {
		return nil, nil
	}
	return &cfg, nil
}

func Save(cfg *AutoDiscoveryConfig) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func ApplyExistingToken(endpoint, autoDiscoveryKey, currentToken string) string {
	if autoDiscoveryKey == "" || currentToken != "" {
		return currentToken
	}
	stored, err := Load()
	if err == nil && stored != nil {
		return stored.Token
	}
	newToken, err := Register(endpoint, autoDiscoveryKey)
	if err != nil {
		return currentToken
	}
	return newToken
}

func Register(endpoint, key string) (string, error) {
	hostname := getHostname()
	url := fmt.Sprintf("%s/api/clients/register?name=%s",
		strings.TrimRight(endpoint, "/"),
		hostname)

	payload := map[string]string{"key": key}
	data, _ := json.Marshal(payload)

	resp, err := http.Post(url, "application/json", strings.NewReader(string(data)))
	if err != nil {
		return "", fmt.Errorf("auto discovery request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result struct {
		Status string `json:"status"`
		Data   struct {
			UUID  string `json:"uuid"`
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse auto discovery response failed: %w", err)
	}
	if result.Status != "success" {
		return "", fmt.Errorf("auto discovery failed: %s", string(body))
	}
	if result.Data.Token == "" {
		return "", fmt.Errorf("auto discovery returned empty token")
	}

	Save(&AutoDiscoveryConfig{
		UUID:  result.Data.UUID,
		Token: result.Data.Token,
	})

	return result.Data.Token, nil
}

func getHostname() string {
	hostname, err := os.Hostname()
	if err == nil && hostname != "" {
		return hostname
	}
	if runtime.GOOS != "windows" {
		data, err := exec.Command("hostname").Output()
		if err == nil {
			if h := strings.TrimSpace(string(data)); h != "" {
				return h
			}
		}
	}
	data, err := os.ReadFile("/etc/hostname")
	if err == nil {
		if h := strings.TrimSpace(string(data)); h != "" {
			return h
		}
	}
	return "komari-agent"
}
