package model

type SensorTemperature struct {
	Name        string
	Temperature float64
}

type HostState struct {
	CPU            float64
	MemUsed        uint64
	SwapUsed       uint64
	DiskUsed       uint64
	NetInTransfer  uint64
	NetOutTransfer uint64
	NetInSpeed     uint64
	NetOutSpeed    uint64
	Uptime         uint64
	Load1          float64
	Load5          float64
	Load15         float64
	TcpConnCount   uint64
	UdpConnCount   uint64
	ProcessCount   uint64
	Temperatures   []SensorTemperature
	GPU            []float64
}

type Host struct {
	Platform        string
	PlatformVersion string
	CPU             []string
	MemTotal        uint64
	DiskTotal       uint64
	SwapTotal       uint64
	Arch            string
	Virtualization  string
	BootTime        uint64
	Version         string
	GPU             []string
}

type GeoIP struct {
	IP          IP     `json:"ip,omitempty"`
	CountryCode string `json:"country_code,omitempty"`
}

type IP struct {
	IPv4Addr string `json:"ipv4_addr,omitempty"`
	IPv6Addr string `json:"ipv6_addr,omitempty"`
}
