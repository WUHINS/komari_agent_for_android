package protocol

import (
	"encoding/json"
	"fmt"
	"time"
)

type ServerMessageKind int

const (
	ServerMessageUnknown  ServerMessageKind = 0
	ServerMessageTerminal ServerMessageKind = 1
	ServerMessageExec     ServerMessageKind = 2
	ServerMessagePing     ServerMessageKind = 3
)

type ServerMessage struct {
	Kind       ServerMessageKind
	Message    string
	RequestID  string
	TaskID     string
	Command    string
	PingTaskID uint64
	PingType   string
	PingTarget string
}

func ParseServerMessage(data []byte) (*ServerMessage, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	msg := &ServerMessage{}

	if v, ok := raw["message"].(string); ok {
		msg.Message = v
	}
	if v, ok := raw["request_id"].(string); ok {
		msg.RequestID = v
	}
	if v, ok := raw["task_id"].(string); ok {
		msg.TaskID = v
	}
	if v, ok := raw["command"].(string); ok {
		msg.Command = v
	}
	if v, ok := raw["ping_type"].(string); ok {
		msg.PingType = v
	}
	if v, ok := raw["ping_target"].(string); ok {
		msg.PingTarget = v
	}

	switch v := raw["ping_task_id"].(type) {
	case float64:
		msg.PingTaskID = uint64(v)
	case string:
		fmt.Sscanf(v, "%d", &msg.PingTaskID)
	}

	if msg.PingTaskID == 0 && msg.Message == "ping" {
		switch v := raw["task_id"].(type) {
		case float64:
			msg.PingTaskID = uint64(v)
		case string:
			fmt.Sscanf(v, "%d", &msg.PingTaskID)
		}
	}

	classifyMessage(msg)
	return msg, nil
}

func classifyMessage(msg *ServerMessage) {
	if msg.Message == "terminal" || msg.RequestID != "" {
		msg.Kind = ServerMessageTerminal
	} else if msg.Message == "exec" {
		msg.Kind = ServerMessageExec
	} else if msg.Message == "ping" || msg.PingTaskID != 0 || msg.PingType != "" || msg.PingTarget != "" {
		msg.Kind = ServerMessagePing
	}
}

type CpuReport struct {
	Usage float64 `json:"usage"`
}

type MemoryReport struct {
	Total uint64 `json:"total"`
	Used  uint64 `json:"used"`
}

type LoadReport struct {
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
}

type DiskReport struct {
	Total uint64 `json:"total"`
	Used  uint64 `json:"used"`
}

type NetworkReport struct {
	Up        uint64 `json:"up"`
	Down      uint64 `json:"down"`
	TotalUp   uint64 `json:"totalUp"`
	TotalDown uint64 `json:"totalDown"`
}

type ConnectionReport struct {
	TCP uint64 `json:"tcp"`
	UDP uint64 `json:"udp"`
}

type ReportPayload struct {
	CPU         CpuReport        `json:"cpu"`
	RAM         MemoryReport     `json:"ram"`
	Swap        MemoryReport     `json:"swap"`
	Load        LoadReport       `json:"load"`
	Disk        DiskReport       `json:"disk"`
	Network     NetworkReport    `json:"network"`
	Connections ConnectionReport `json:"connections"`
	Uptime      uint64           `json:"uptime"`
	Process     uint64           `json:"process"`
	Message     string           `json:"message"`
}

func (r *ReportPayload) JSON() []byte {
	data, _ := json.Marshal(r)
	return data
}

type BasicInfoPayload struct {
	CPUName        string `json:"cpu_name"`
	CPUCores       uint32 `json:"cpu_cores"`
	Arch           string `json:"arch"`
	OS             string `json:"os"`
	KernelVersion  string `json:"kernel_version,omitempty"`
	IPv4           string `json:"ipv4"`
	IPv6           string `json:"ipv6"`
	MemTotal       uint64 `json:"mem_total"`
	SwapTotal      uint64 `json:"swap_total"`
	DiskTotal      uint64 `json:"disk_total"`
	GPUName        string `json:"gpu_name"`
	Virtualization string `json:"virtualization"`
	Version        string `json:"version"`
}

func (b *BasicInfoPayload) JSON() []byte {
	data, _ := json.Marshal(b)
	return data
}

type TaskResultPayload struct {
	TaskID     string `json:"task_id"`
	Result     string `json:"result"`
	ExitCode   int32  `json:"exit_code"`
	FinishedAt string `json:"finished_at"`
}

func (t *TaskResultPayload) JSON() []byte {
	data, _ := json.Marshal(t)
	return data
}

type PingResultPayload struct {
	Type       string `json:"type"`
	TaskID     uint64 `json:"task_id"`
	PingType   string `json:"ping_type"`
	Value      int64  `json:"value"`
	FinishedAt string `json:"finished_at"`
}

func (p *PingResultPayload) JSON() []byte {
	data, _ := json.Marshal(p)
	return data
}

type AutoDiscoveryRequest struct {
	Key string `json:"key"`
}

func (a *AutoDiscoveryRequest) JSON() []byte {
	data, _ := json.Marshal(a)
	return data
}

func UTCTimeNow() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}
