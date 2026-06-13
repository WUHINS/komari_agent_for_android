package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/blang/semver"
	"github.com/nezhahq/service"
	ping "github.com/prometheus-community/pro-bing"
	utls "github.com/refraction-networking/utls"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/urfave/cli/v2"

	"github.com/komari-monitor/komari-agent/cmd/agent/commands"
	"github.com/komari-monitor/komari-agent/model"
	"github.com/komari-monitor/komari-agent/pkg/discovery"
	"github.com/komari-monitor/komari-agent/pkg/fsnotifyx"
	"github.com/komari-monitor/komari-agent/pkg/logger"
	"github.com/komari-monitor/komari-agent/pkg/monitor"
	"github.com/komari-monitor/komari-agent/pkg/processgroup"
	"github.com/komari-monitor/komari-agent/pkg/pty"
	"github.com/komari-monitor/komari-agent/pkg/util"
	utlsx "github.com/komari-monitor/komari-agent/pkg/utls"
	"github.com/komari-monitor/komari-agent/pkg/ws"
)

var (
	version               = monitor.Version
	arch                  string
	executablePath        string
	defaultConfigPath     = loadDefaultConfigPath()
	agentConfig           *model.AgentConfig
	initialized           bool
	prevDashboardBootTime uint64
	geoipReported         bool
	lastReportHostInfo    time.Time
	lastReportIPInfo      time.Time

	hostStatus   atomic.Bool
	ipStatus     atomic.Bool
	reloadStatus atomic.Bool

	dnsResolver = &net.Resolver{PreferGo: true}
	httpClient  = &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: time.Second * 30,
	}

	reloadSigChan = make(chan struct{})
	shutdownReq   atomic.Bool
)

var (
	println = logger.Println
	printf  = logger.Printf
)

const (
	delayWhenError    = time.Second * 10
	networkTimeOut    = time.Second * 5
	minUpdateInterval = 1440
	maxUpdateInterval = 2880
	binaryName        = "komari-agent"
)

func setEnv() {
	net.DefaultResolver.PreferGo = true
	net.DefaultResolver.Dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		d := net.Dialer{Timeout: time.Second * 5}
		dnsServers := util.DNSServersAll
		if len(agentConfig.DNS) > 0 {
			dnsServers = agentConfig.DNS
		}
		var conn net.Conn
		var err error
		for _, server := range util.RangeRnd(dnsServers) {
			conn, err = d.DialContext(ctx, "udp", server)
			if err == nil {
				return conn, nil
			}
		}
		return nil, err
	}
	headers := util.BrowserHeaders()
	http.DefaultClient.Timeout = time.Second * 30
	httpClient.Transport = utlsx.NewUTLSHTTPRoundTripperWithProxy(
		utls.HelloChrome_Auto, new(utls.Config),
		http.DefaultTransport, nil, headers,
	)
}

func loadDefaultConfigPath() string {
	var err error
	executablePath, err = os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(executablePath), "config.json")
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func runCLI() {
	app := &cli.App{
		Usage:   "Komari Agent for Android",
		Version: version,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config", Aliases: []string{"c"}, Usage: "配置文件路径"},
			&cli.StringFlag{Name: "endpoint", Aliases: []string{"e"}, Usage: "服务器地址"},
			&cli.StringFlag{Name: "token", Aliases: []string{"t"}, Usage: "客户端密钥"},
			&cli.BoolFlag{Name: "disable-auto-update", Usage: "关闭自动更新"},
		},
		Action: func(c *cli.Context) error {
			cfg, err := model.ParseArgs(os.Args)
			if err != nil {
				return err
			}

			configPath := c.String("config")
			if configPath == "" {
				configPath = cfg.ConfigFile
			}
			if configPath == "" {
				configPath = defaultConfigPath
			}

			setEnv()

			fileCfg, err := model.LoadConfig(configPath)
			if err == nil && fileCfg != nil {
				cfg.Endpoint = coalesce(cfg.Endpoint, fileCfg.Endpoint)
				cfg.Token = coalesce(cfg.Token, fileCfg.Token)
				if !cfg.GPU {
					cfg.GPU = fileCfg.GPU
				}
				if cfg.Interval == 1.0 && fileCfg.Interval != 1.0 {
					cfg.Interval = fileCfg.Interval
				}
				if cfg.MaxRetries == 3 && fileCfg.MaxRetries != 3 {
					cfg.MaxRetries = fileCfg.MaxRetries
				}
				cfg.DisableAutoUpdate = cfg.DisableAutoUpdate || fileCfg.DisableAutoUpdate
				cfg.DisableCommand = cfg.DisableCommand || fileCfg.DisableCommand
				cfg.DisableNAT = cfg.DisableNAT || fileCfg.DisableNAT
				cfg.Debug = cfg.Debug || fileCfg.Debug
			}

			agentConfig = cfg
			monitor.InitConfig(agentConfig)
			monitor.CustomEndpoints = agentConfig.CustomIPApi

			if agentConfig.ShowWarning {
				return nil
			}

			runService("", "")
			return nil
		},
		Commands: []*cli.Command{
			{
				Name:  "edit",
				Usage: "编辑配置文件",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config", Aliases: []string{"c"}, Usage: "配置文件路径"},
				},
				Action: func(c *cli.Context) error {
					path := c.String("config")
					if path == "" {
						path = defaultConfigPath
					}
					commands.EditAgentConfig(path, &model.AgentConfig{})
					return nil
				},
			},
			{
				Name:      "service",
				Usage:     "服务操作",
				UsageText: "<install/uninstall/start/stop/restart>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config", Aliases: []string{"c"}, Usage: "配置文件路径"},
				},
				Action: func(c *cli.Context) error {
					if arg := c.Args().Get(0); arg != "" {
						path := c.String("config")
						if path == "" {
							path = defaultConfigPath
						}
						ap, _ := filepath.Abs(path)
						runService(arg, ap)
						return nil
					}
					return cli.Exit("必须指定一个参数", 1)
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}

	if agentConfig.AutoDiscoveryKey != "" || agentConfig.Token == "" {
		if token := discovery.ApplyExistingToken(agentConfig.Endpoint, agentConfig.AutoDiscoveryKey, agentConfig.Token); token != "" && token != agentConfig.Token {
			agentConfig.Token = token
			printf("Auto discovery token applied")
		}
	}

	if _, err := semver.Parse(version); err == nil && !agentConfig.DisableAutoUpdate {
		if doSelfUpdate(true) {
			os.Exit(1)
		}
		go func() {
			var interval time.Duration
			if agentConfig.SelfUpdatePeriod > 0 {
				interval = time.Duration(agentConfig.SelfUpdatePeriod) * time.Minute
			} else {
				interval = time.Duration(rand.Intn(maxUpdateInterval-minUpdateInterval)+minUpdateInterval) * time.Minute
			}
			for range time.Tick(interval) {
				if doSelfUpdate(true) {
					os.Exit(1)
				}
			}
		}()
	}

	uploadBasicInfo()
	go func() {
		mins := agentConfig.InfoReportPeriod
		if mins <= 0 {
			mins = 5
		}
		for range time.Tick(time.Duration(mins) * time.Minute) {
			uploadBasicInfo()
		}
	}()

	wsCfg := ws.Config{
		Endpoint:         agentConfig.Endpoint,
		Token:            agentConfig.Token,
		IgnoreUnsafeCert: agentConfig.InsecureTLS,
		Debug:            agentConfig.Debug,
	}

	for !shutdownReq.Load() {
		func() {
			client := ws.NewClient(wsCfg)
			if err := client.Connect(ctx); err != nil {
				printf("WebSocket连接失败: %v", err)
				time.Sleep(delayWhenError)
				return
			}
			defer client.Close()
			printf("WebSocket连接成功: %s", agentConfig.Endpoint)
			initialized = true

			ctxWs, cancel := context.WithCancel(ctx)
			defer cancel()

			go readerLoop(ctxWs, client)

			lastHeartbeat := time.Now()
			for !shutdownReq.Load() {
				select {
				case <-reloadSigChan:
					println("重载配置...")
					cancel()
					return
				case <-ctxWs.Done():
					return
				default:
				}

				if time.Since(lastHeartbeat) >= 30*time.Second {
					if err := client.WritePing(); err != nil {
						printf("心跳发送失败: %v", err)
						break
					}
					lastHeartbeat = time.Now()
				}

				reportState()
				time.Sleep(intervalDuration())
			}
		}()
		time.Sleep(delayWhenError)
	}
}

func intervalDuration() time.Duration {
	ms := int64(agentConfig.Interval * 1000)
	if ms < 100 {
		ms = 1000
	}
	return time.Duration(ms) * time.Millisecond
}

func readerLoop(ctx context.Context, client *ws.Client) {
	defer func() {
		if r := recover(); r != nil {
			printf("readerLoop panic: %v", r)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		data, err := client.ReadText()
		if err != nil {
			if !shutdownReq.Load() {
				printf("WebSocket读取失败: %v", err)
			}
			return
		}

		handleServerMessage(client, data)
	}
}

func handleServerMessage(client *ws.Client, data []byte) {
	msg, err := parseServerMessage(data)
	if err != nil {
		printf("解析服务器消息失败: %v", err)
		return
	}

	switch msg.Kind {
	case serverMsgPing:
		go handlePingTask(client, msg)
	case serverMsgExec:
		go handleExecTask(client, msg)
	case serverMsgTerminal:
		go handleTerminalTaskWS(client, msg)
	default:
		printf("未知消息类型: %s", string(data))
	}
}

type serverMsgKind int

const (
	serverMsgUnknown  serverMsgKind = 0
	serverMsgPing     serverMsgKind = 1
	serverMsgExec     serverMsgKind = 2
	serverMsgTerminal serverMsgKind = 3
)

type serverMessage struct {
	Kind       serverMsgKind
	Message    string
	RequestID  string
	TaskID     string
	Command    string
	PingTaskID uint64
	PingType   string
	PingTarget string
}

func parseServerMessage(data []byte) (*serverMessage, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	msg := &serverMessage{}

	if v, ok := raw["message"].(string); ok {
		msg.Message = v
	}
	if v, ok := raw["request_id"].(string); ok {
		msg.RequestID = v
	}
	if v, ok := raw["task_id"].(string); ok {
		msg.TaskID = v
	} else if v, ok := raw["task_id"].(float64); ok {
		msg.TaskID = fmt.Sprintf("%.0f", v)
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

func classifyMessage(msg *serverMessage) {
	if msg.Message == "terminal" || msg.RequestID != "" {
		msg.Kind = serverMsgTerminal
	} else if msg.Message == "exec" {
		msg.Kind = serverMsgExec
	} else if msg.Message == "ping" || msg.PingTaskID != 0 || msg.PingType != "" || msg.PingTarget != "" {
		msg.Kind = serverMsgPing
	}
}

func reportState() {
	if !initialized {
		return
	}
	monitor.TrackNetworkSpeed()

	hostState := monitor.GetState(agentConfig.SkipConnCount, agentConfig.SkipProcsCount)
	report := map[string]interface{}{
		"cpu": map[string]interface{}{
			"usage": hostState.CPU,
		},
		"ram": map[string]interface{}{
			"total": hostState.MemUsed + getMemFree(),
			"used":  hostState.MemUsed,
		},
		"swap": map[string]interface{}{
			"total": hostState.SwapUsed + getSwapFree(),
			"used":  hostState.SwapUsed,
		},
		"load": map[string]interface{}{
			"load1":  hostState.Load1,
			"load5":  hostState.Load5,
			"load15": hostState.Load15,
		},
		"disk": map[string]interface{}{
			"total": hostState.DiskUsed + getDiskFree(),
			"used":  hostState.DiskUsed,
		},
		"network": map[string]interface{}{
			"up":        hostState.NetOutSpeed,
			"down":      hostState.NetInSpeed,
			"totalUp":   hostState.NetOutTransfer,
			"totalDown": hostState.NetInTransfer,
		},
		"connections": map[string]interface{}{
			"tcp": hostState.TcpConnCount,
			"udp": hostState.UdpConnCount,
		},
		"uptime":  hostState.Uptime,
		"process": hostState.ProcessCount,
		"message": "",
	}

	data, err := json.Marshal(report)
	if err != nil {
		printf("序列化报告失败: %v", err)
		return
	}

	if agentConfig.Debug {
		printf("上报状态: %s", string(data[:min(len(data), 200)]))
	}
}

func getMemFree() uint64 {
	return 0
}

func getSwapFree() uint64 {
	return 0
}

func getDiskFree() uint64 {
	return 0
}

func uploadBasicInfo() {
	hostInfo := monitor.GetHost()
	hi, err := host.Info()
	kernelVersion := ""
	if err == nil {
		kernelVersion = hi.KernelVersion
	}

	var cpuCores uint32
	for _, c := range hostInfo.CPU {
		fmt.Sscanf(c, "%*s %d", &cpuCores)
	}
	if cpuCores == 0 {
		cpuCores = 1
	}

	info := map[string]interface{}{
		"cpu_name":       strings.Join(hostInfo.CPU, ", "),
		"cpu_cores":      cpuCores,
		"arch":           hostInfo.Arch,
		"os":             hostInfo.Platform,
		"kernel_version": kernelVersion,
		"ipv4":           "",
		"ipv6":           "",
		"mem_total":      hostInfo.MemTotal,
		"swap_total":     hostInfo.SwapTotal,
		"disk_total":     hostInfo.DiskTotal,
		"gpu_name":       strings.Join(hostInfo.GPU, ", "),
		"virtualization": hostInfo.Virtualization,
		"version":        version,
	}

	data, err := json.Marshal(info)
	if err != nil {
		printf("序列化基本信息失败: %v", err)
		return
	}

	url := fmt.Sprintf("%s/api/clients/uploadBasicInfo?token=%s",
		strings.TrimRight(agentConfig.Endpoint, "/"),
		agentConfig.Token)

	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		printf("上传基本信息失败: %v", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode == 200 {
		printf("基本信息上传成功")
	}
}

func handlePingTask(client *ws.Client, msg *serverMessage) {
	if agentConfig.DisableSendQuery {
		return
	}

	var value int64
	ipAddr, err := lookupIP(msg.PingTarget)
	if err != nil {
		value = -1
	} else {
		switch msg.PingType {
		case "icmp":
			pinger, err := ping.NewPinger(ipAddr)
			if err == nil {
				pinger.SetPrivileged(true)
				pinger.Count = 5
				pinger.Timeout = time.Second * 20
				err = pinger.Run()
				if err == nil {
					stat := pinger.Statistics()
					if stat.PacketsRecv > 0 {
						value = stat.AvgRtt.Microseconds() / 1000
					} else {
						value = -1
					}
				}
			}
		case "tcp":
			start := time.Now()
			conn, err := net.DialTimeout("tcp", ipAddr+":80", time.Second*10)
			if err == nil {
				conn.Close()
				value = time.Since(start).Microseconds() / 1000
			} else {
				value = -1
			}
		case "http":
			start := time.Now()
			resp, err := httpClient.Get("http://" + ipAddr)
			if err == nil {
				resp.Body.Close()
				value = time.Since(start).Microseconds() / 1000
			} else {
				value = -1
			}
		default:
			value = -1
		}
	}

	finished := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	result := map[string]interface{}{
		"type":        "ping_result",
		"task_id":     msg.PingTaskID,
		"ping_type":   msg.PingType,
		"value":       value,
		"finished_at": finished,
	}
	data, _ := json.Marshal(result)
	client.WriteText(data)
}

func handleExecTask(client *ws.Client, msg *serverMessage) {
	if agentConfig.DisableCommand || msg.TaskID == "" {
		return
	}

	result := runCommand(msg.Command)
	finished := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	payload := map[string]interface{}{
		"task_id":     msg.TaskID,
		"result":      result,
		"exit_code":   0,
		"finished_at": finished,
	}
	data, _ := json.Marshal(payload)

	url := fmt.Sprintf("%s/api/clients/task/result?token=%s",
		strings.TrimRight(agentConfig.Endpoint, "/"),
		agentConfig.Token)
	httpClient.Post(url, "application/json", bytes.NewReader(data))
}

func runCommand(command string) string {
	if command == "" {
		return "No command provided"
	}
	var b bytes.Buffer
	var errBuf bytes.Buffer
	cmd := processgroup.NewCommand(command)
	cmd.Stdout = &b
	cmd.Stderr = &errBuf
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return err.Error()
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Hour):
		processgroup.NewProcessExitGroup()
		return "任务执行超时"
	}
	if errBuf.Len() > 0 {
		b.WriteString("\n" + errBuf.String())
	}
	return b.String()
}

func handleTerminalTaskWS(client *ws.Client, msg *serverMessage) {
	if agentConfig.DisableCommand {
		return
	}

	tty, err := pty.Start()
	if err != nil {
		printf("终端启动失败: %v", err)
		return
	}
	defer tty.Close()

	termClient := ws.NewClient(ws.Config{
		Endpoint:         fmt.Sprintf("%s/api/clients/terminal?token=%s&id=%s", strings.TrimRight(agentConfig.Endpoint, "/"), agentConfig.Token, msg.RequestID),
		Token:            agentConfig.Token,
		IgnoreUnsafeCert: agentConfig.InsecureTLS,
	})
	if err := termClient.Connect(context.Background()); err != nil {
		printf("终端WS连接失败: %v", err)
		return
	}
	defer termClient.Close()

	go func() {
		buf := make([]byte, 10240)
		for {
			n, err := tty.Read(buf)
			if err != nil {
				return
			}
			termClient.WriteText(buf[:n])
		}
	}()

	for {
		data, err := termClient.ReadText()
		if err != nil {
			return
		}
		if len(data) == 0 {
			continue
		}
		tty.Write(data)
	}
}

func runService(action string, path string) {
	winConfig := map[string]interface{}{
		"OnFailure": "restart",
	}

	args := []string{"-c", path}
	name := filepath.Base(executablePath)
	if path != defaultConfigPath && path != "" {
		hex := util.MD5Sum(path)[:7]
		name = fmt.Sprintf("%s-%s", name, hex)
	}

	svcConfig := &service.Config{
		Name:             name,
		DisplayName:      filepath.Base(executablePath),
		Arguments:        args,
		Description:      "Komari Agent",
		WorkingDirectory: filepath.Dir(executablePath),
		Option:           winConfig,
	}

	prg := &commands.Program{
		Exit: make(chan struct{}),
		Run:  func() { run(context.Background()) },
	}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		printf("创建服务时出错，以普通模式运行: %v", err)
		run(context.Background())
		return
	}
	prg.Service = s

	serviceLogger, err := logger.NewServiceLoggerFromService(s, nil)
	if err != nil {
		logger.InitDefaultLogger(agentConfig.Debug, service.ConsoleLogger)
	} else {
		logger.InitDefaultLogger(agentConfig.Debug, serviceLogger)
	}

	if action == "install" {
		if err := agentConfig.Save(); err != nil {
			log.Fatalf("init config failed: %v", err)
		}
	}

	if len(action) != 0 {
		err := service.Control(s, action)
		if err != nil {
			log.Fatal(err)
		}
		return
	}

	err = s.Run()
	if err != nil {
		logger.Error(err)
	}
}

func doSelfUpdate(useLocalVersion bool) (exit bool) {
	v := semver.MustParse("0.1.0")
	if useLocalVersion {
		vr, err := semver.Parse(version)
		if err != nil {
			printf("failed to parse current version string: %v", err)
			return
		}
		cmd := exec.Command(executablePath, "-v")
		vb, err := cmd.Output()
		if err != nil {
			printf("failed to retrieve current executable version: %v", err)
			return
		}
		vraw := strings.Split(strings.TrimSpace(string(vb)), " ")
		vstr := vraw[len(vraw)-1]
		v, err = semver.Parse(vstr)
		if err != nil {
			printf("failed to parse executable version string: %v", err)
			return
		}
		if !vr.Equals(v) {
			printf("executable version differs from current version, exiting to re-check update...")
			exit = true
			return
		}
	}

	execHash := util.MD5Sum(executablePath)[:7]
	statName := fmt.Sprintf("agent-%s.stat", execHash)
	tmpDir := filepath.Join(os.TempDir(), binaryName)
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		printf("failed to create temp dir: %v", err)
		return
	}

	statFile := filepath.Join(tmpDir, statName)
	if _, err := os.Stat(statFile); err == nil {
		printf("found self-update stat file, waiting for another process to finish update...")
		if fErr := fsnotifyx.ExitOnDeleteFile(context.Background(), printf, statFile); fErr != nil {
			if errors.Is(fErr, fsnotifyx.ErrTimeout) {
				os.Remove(statFile)
			}
			printf("failed to monitor path of stat file: %v", fErr)
			return
		}
		exit = true
		return
	} else {
		if !errors.Is(err, os.ErrNotExist) {
			printf("failed to retrieve self-update stat at %s", statFile)
			return
		}
	}

	stat, err := os.OpenFile(statFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		printf("failed to create self-update stat file: %v", err)
		return
	}

	defer func() {
		stat.Close()
		if err := os.Remove(statFile); err != nil {
			printf("remove stat failed: %v", err)
		}
	}()

	printf("检查更新: %v", v)

	repo := "komari-monitor/komari-agent"
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	resp, err := httpClient.Get(url)
	if err != nil {
		printf("检查更新失败: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		printf("检查更新失败: HTTP %d", resp.StatusCode)
		return
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		printf("解析更新信息失败: %v", err)
		return
	}

	latestVersion, err := semver.Parse(strings.TrimPrefix(release.TagName, "v"))
	if err != nil {
		printf("解析最新版本失败: %v", err)
		return
	}

	if latestVersion.LTE(v) {
		printf("已经是最新版本: %v", v)
		return
	}

	printf("发现新版本: %s", release.TagName)

	arch := "linux_amd64"
	if runtime.GOARCH == "arm64" {
		arch = "linux_arm64"
	} else if runtime.GOARCH == "386" {
		arch = "linux_386"
	}

	var downloadURL string
	for _, asset := range release.Assets {
		if strings.Contains(asset.Name, arch) {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		printf("未找到匹配当前架构的更新文件: %s", arch)
		return
	}

	binResp, err := httpClient.Get(downloadURL)
	if err != nil {
		printf("下载更新失败: %v", err)
		return
	}
	defer binResp.Body.Close()

	binData, err := io.ReadAll(binResp.Body)
	if err != nil {
		printf("读取更新数据失败: %v", err)
		return
	}

	tmpFile := filepath.Join(tmpDir, binaryName+".new")
	if err := os.WriteFile(tmpFile, binData, 0755); err != nil {
		printf("写入临时文件失败: %v", err)
		return
	}

	if err := os.Rename(tmpFile, executablePath); err != nil {
		os.Remove(tmpFile)
		printf("替换二进制文件失败: %v", err)
		return
	}

	printf("已经更新至: %s, 正在结束进程", release.TagName)
	exit = true
	return
}

func lookupIP(hostOrIp string) (string, error) {
	if net.ParseIP(hostOrIp) == nil {
		ips, err := dnsResolver.LookupIPAddr(context.Background(), hostOrIp)
		if err != nil {
			return "", err
		}
		if len(ips) == 0 {
			return "", fmt.Errorf("无法解析 %s", hostOrIp)
		}
		return ips[0].IP.String(), nil
	}
	return hostOrIp, nil
}
