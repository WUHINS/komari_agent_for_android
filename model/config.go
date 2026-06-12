package model

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Command int

const (
	CommandRun Command = iota
	CommandListDisk
	CommandCheckMem
)

type AgentConfig struct {
	Debug bool `json:"debug"`

	// Connection
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	UUID     string `json:"uuid"`

	// Report
	Interval          float64 `json:"interval"`
	InfoReportPeriod  int     `json:"info_report_period"`
	MaxRetries        int     `json:"max_retries"`
	ReconnectInterval int     `json:"reconnect_interval"`

	// Features
	DisableAutoUpdate bool `json:"disable_auto_update"`
	DisableCommand    bool `json:"disable_command"`
	DisableWebSSH     bool `json:"disable_web_ssh"`
	DisableNAT        bool `json:"disable_nat"`
	DisableSendQuery  bool `json:"disable_send_query"`

	// Monitoring
	GPU             bool   `json:"gpu"`
	Temperature     bool   `json:"temperature"`
	SkipConnCount   bool   `json:"skip_conn_count"`
	SkipProcsCount  bool   `json:"skip_procs_count"`

	// Network
	NICAllowlist map[string]bool `json:"nic_allowlist,omitempty"`
	ExcludeNICs  string          `json:"exclude_nics,omitempty"`
	IncludeNICs  string          `json:"include_nics,omitempty"`
	DNS          []string        `json:"dns,omitempty"`
	CustomDNS    string          `json:"custom_dns,omitempty"`

	// Disk
	HardDrivePartitionAllowlist []string `json:"hard_drive_partition_allowlist,omitempty"`
	IncludeMountpoints          string   `json:"include_mountpoints,omitempty"`

	// IP
	CustomIPv4      string `json:"custom_ipv4,omitempty"`
	CustomIPv6      string `json:"custom_ipv6,omitempty"`
	GetIPFromNIC    bool   `json:"get_ip_from_nic"`
	UseIPv6Country  bool   `json:"use_ipv6_country"`
	CustomIPApi     []string `json:"custom_ip_api,omitempty"`

	// TLS
	TLS         bool `json:"tls"`
	InsecureTLS bool `json:"insecure_tls"`

	// Update
	DisableForceUpdate bool   `json:"disable_force_update"`
	SelfUpdatePeriod   int    `json:"self_update_period"`

	// Auto discovery
	AutoDiscoveryKey string `json:"auto_discovery_key,omitempty"`

	// Cloudflare Access
	CFAccessClientID     string `json:"cf_access_client_id,omitempty"`
	CFAccessClientSecret string `json:"cf_access_client_secret,omitempty"`

	// Memory
	MemoryIncludeCache   bool `json:"memory_include_cache"`
	MemoryReportRawUsed  bool `json:"memory_report_raw_used"`

	// Misc
	MonthRotate  int    `json:"month_rotate"`
	ShowWarning  bool   `json:"show_warning"`
	HostProc     string `json:"host_proc,omitempty"`
	ConfigFile   string `json:"config_file,omitempty"`

	// Internal
	command Command
}

func DefaultConfig() *AgentConfig {
	return &AgentConfig{
		Interval:          1.0,
		InfoReportPeriod:  5,
		MaxRetries:        3,
		ReconnectInterval: 5,
	}
}

func LoadConfig(path string) (*AgentConfig, error) {
	cfg := DefaultConfig()

	if path != "" {
		data, err := os.ReadFile(path)
		if err == nil {
			if err := json.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("failed to parse config file: %w", err)
			}
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	cfg.loadEnv()

	return cfg, nil
}

func ParseArgs(args []string) (*AgentConfig, error) {
	cfg := DefaultConfig()

	for i := 1; i < len(args); i++ {
		arg := args[i]

		switch {
		case arg == "list-disk":
			cfg.command = CommandListDisk
		case arg == "check-mem":
			cfg.command = CommandCheckMem
		case arg == "--disable-auto-update" || arg == "--disable-auto-update=true":
			cfg.DisableAutoUpdate = true
		case arg == "--disable-web-ssh" || arg == "--disable-web-ssh=true":
			cfg.DisableWebSSH = true
		case strings.HasPrefix(arg, "--token="):
			cfg.Token = strings.TrimPrefix(arg, "--token=")
		case arg == "-t" || arg == "--token":
			if v := nextArg(args, &i); v != "" {
				cfg.Token = v
			}
		case strings.HasPrefix(arg, "--endpoint="):
			cfg.Endpoint = strings.TrimPrefix(arg, "--endpoint=")
		case arg == "-e" || arg == "--endpoint":
			if v := nextArg(args, &i); v != "" {
				cfg.Endpoint = v
			}
		case strings.HasPrefix(arg, "--interval="):
			v := strings.TrimPrefix(arg, "--interval=")
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				cfg.Interval = f
			}
		case arg == "-i" || arg == "--interval":
			if v := nextArg(args, &i); v != "" {
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					cfg.Interval = f
				}
			}
		case strings.HasPrefix(arg, "--max-retries="):
			v := strings.TrimPrefix(arg, "--max-retries=")
			if n, err := strconv.Atoi(v); err == nil {
				cfg.MaxRetries = n
			}
		case strings.HasPrefix(arg, "--reconnect-interval="):
			v := strings.TrimPrefix(arg, "--reconnect-interval=")
			if n, err := strconv.Atoi(v); err == nil {
				cfg.ReconnectInterval = n
			}
		case strings.HasPrefix(arg, "--info-report-interval="):
			v := strings.TrimPrefix(arg, "--info-report-interval=")
			if n, err := strconv.Atoi(v); err == nil {
				cfg.InfoReportPeriod = n
			}
		case strings.HasPrefix(arg, "--include-nics="):
			cfg.IncludeNICs = strings.TrimPrefix(arg, "--include-nics=")
		case strings.HasPrefix(arg, "--exclude-nics="):
			cfg.ExcludeNICs = strings.TrimPrefix(arg, "--exclude-nics=")
		case strings.HasPrefix(arg, "--include-mountpoint="):
			cfg.IncludeMountpoints = strings.TrimPrefix(arg, "--include-mountpoint=")
		case strings.HasPrefix(arg, "--month-rotate="):
			v := strings.TrimPrefix(arg, "--month-rotate=")
			if n, err := strconv.Atoi(v); err == nil {
				cfg.MonthRotate = n
			}
		case strings.HasPrefix(arg, "--cf-access-client-id="):
			cfg.CFAccessClientID = strings.TrimPrefix(arg, "--cf-access-client-id=")
		case strings.HasPrefix(arg, "--cf-access-client-secret="):
			cfg.CFAccessClientSecret = strings.TrimPrefix(arg, "--cf-access-client-secret=")
		case strings.HasPrefix(arg, "--custom-dns="):
			cfg.CustomDNS = strings.TrimPrefix(arg, "--custom-dns=")
		case strings.HasPrefix(arg, "--custom-ipv4="):
			cfg.CustomIPv4 = strings.TrimPrefix(arg, "--custom-ipv4=")
		case strings.HasPrefix(arg, "--custom-ipv6="):
			cfg.CustomIPv6 = strings.TrimPrefix(arg, "--custom-ipv6=")
		case strings.HasPrefix(arg, "--config="):
			cfg.ConfigFile = strings.TrimPrefix(arg, "--config=")
		case strings.HasPrefix(arg, "--auto-discovery="):
			cfg.AutoDiscoveryKey = strings.TrimPrefix(arg, "--auto-discovery=")
		case arg == "-u" || arg == "--ignore-unsafe-cert":
			cfg.InsecureTLS = true
		case arg == "--gpu":
			cfg.GPU = true
		case arg == "--debug-log":
			cfg.Debug = true
		case arg == "--show-warning":
			cfg.ShowWarning = true
		case arg == "--get-ip-addr-from-nic":
			cfg.GetIPFromNIC = true
		case arg == "--memory-include-cache":
			cfg.MemoryIncludeCache = true
		case arg == "--memory-exclude-bcf":
			cfg.MemoryReportRawUsed = true
		case strings.HasPrefix(arg, "--host-proc="):
			cfg.HostProc = strings.TrimPrefix(arg, "--host-proc=")
		default:
			if strings.HasPrefix(arg, "-") {
				nextArg(args, &i)
			}
		}
	}

	cfg.loadEnv()

	if cfg.ConfigFile != "" {
		data, err := os.ReadFile(cfg.ConfigFile)
		if err == nil {
			json.Unmarshal(data, cfg)
		}
	}

	return cfg, nil
}

func (c *AgentConfig) loadEnv() {
	if v := os.Getenv("AGENT_ENDPOINT"); v != "" {
		c.Endpoint = v
	}
	if v := os.Getenv("AGENT_TOKEN"); v != "" {
		c.Token = v
	}
	if v := os.Getenv("AGENT_AUTO_DISCOVERY_KEY"); v != "" {
		c.AutoDiscoveryKey = v
	}
	if v := os.Getenv("AGENT_INTERVAL"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.Interval = f
		}
	}
	if v := os.Getenv("AGENT_MAX_RETRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.MaxRetries = n
		}
	}
	if v := os.Getenv("AGENT_RECONNECT_INTERVAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.ReconnectInterval = n
		}
	}
	if v := os.Getenv("AGENT_INFO_REPORT_INTERVAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.InfoReportPeriod = n
		}
	}
	if v := os.Getenv("AGENT_DISABLE_AUTO_UPDATE"); v == "true" || v == "1" {
		c.DisableAutoUpdate = true
	}
	if v := os.Getenv("AGENT_DISABLE_WEB_SSH"); v == "true" || v == "1" {
		c.DisableWebSSH = true
	}
	if v := os.Getenv("AGENT_DEBUG_LOG"); v == "true" || v == "1" {
		c.Debug = true
	}
	if v := os.Getenv("AGENT_INCLUDE_NICS"); v != "" {
		c.IncludeNICs = v
	}
	if v := os.Getenv("AGENT_EXCLUDE_NICS"); v != "" {
		c.ExcludeNICs = v
	}
	if v := os.Getenv("AGENT_INCLUDE_MOUNTPOINTS"); v != "" {
		c.IncludeMountpoints = v
	}
	if v := os.Getenv("AGENT_MONTH_ROTATE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.MonthRotate = n
		}
	}
	if v := os.Getenv("AGENT_CUSTOM_DNS"); v != "" {
		c.CustomDNS = v
	}
	if v := os.Getenv("AGENT_ENABLE_GPU"); v == "true" || v == "1" {
		c.GPU = true
	}
	if v := os.Getenv("AGENT_CUSTOM_IPV4"); v != "" {
		c.CustomIPv4 = v
	}
	if v := os.Getenv("AGENT_CUSTOM_IPV6"); v != "" {
		c.CustomIPv6 = v
	}
	if v := os.Getenv("AGENT_GET_IP_ADDR_FROM_NIC"); v == "true" || v == "1" {
		c.GetIPFromNIC = true
	}
	if v := os.Getenv("AGENT_IGNORE_UNSAFE_CERT"); v == "true" || v == "1" {
		c.InsecureTLS = true
	}
	if v := os.Getenv("AGENT_CF_ACCESS_CLIENT_ID"); v != "" {
		c.CFAccessClientID = v
	}
	if v := os.Getenv("AGENT_CF_ACCESS_CLIENT_SECRET"); v != "" {
		c.CFAccessClientSecret = v
	}
	if v := os.Getenv("AGENT_MEMORY_INCLUDE_CACHE"); v == "true" || v == "1" {
		c.MemoryIncludeCache = true
	}
	if v := os.Getenv("AGENT_MEMORY_REPORT_RAW_USED"); v == "true" || v == "1" {
		c.MemoryReportRawUsed = true
	}
	if v := os.Getenv("AGENT_CONFIG_FILE"); v != "" {
		c.ConfigFile = v
	}
	if v := os.Getenv("AGENT_SHOW_WARNING"); v == "true" || v == "1" {
		c.ShowWarning = true
	}
	if v := os.Getenv("HOST_PROC"); v != "" {
		c.HostProc = v
	}
}

func nextArg(args []string, i *int) string {
	if *i+1 >= len(args) {
		return ""
	}
	n := args[*i+1]
	if strings.HasPrefix(n, "-") {
		return ""
	}
	*i++
	return n
}

func (c *AgentConfig) Save() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	path := c.ConfigFile
	if path == "" {
		path = "config.json"
	}
	return os.WriteFile(path, data, 0600)
}

func (c *AgentConfig) Read(path string) error {
	cfg, err := LoadConfig(path)
	if err != nil {
		return err
	}
	*c = *cfg
	return nil
}
