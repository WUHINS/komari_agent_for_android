//go:build gui || android

package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/hashicorp/go-uuid"

	"github.com/komari-monitor/komari-agent/model"
	"github.com/komari-monitor/komari-agent/pkg/logger"
	"github.com/komari-monitor/komari-agent/pkg/monitor"
)

var (
	isAgentRunning bool               // Agent 是否正在运行
	agentCancel    context.CancelFunc // Agent 上下文取消函数
)

// GUILogger 实现服务日志系统并将其实时输出于 Fyne 列表组件中
type GUILogger struct {
	writer func(msg string)
}

func (l *GUILogger) Error(v ...interface{}) error {
	l.writer(fmt.Sprint(v...))
	return nil
}
func (l *GUILogger) Warning(v ...interface{}) error {
	l.writer(fmt.Sprint(v...))
	return nil
}
func (l *GUILogger) Info(v ...interface{}) error {
	l.writer(fmt.Sprint(v...))
	return nil
}
func (l *GUILogger) Errorf(format string, a ...interface{}) error {
	l.writer(fmt.Sprintf(format, a...))
	return nil
}
func (l *GUILogger) Warningf(format string, a ...interface{}) error {
	l.writer(fmt.Sprintf(format, a...))
	return nil
}
func (l *GUILogger) Infof(format string, a ...interface{}) error {
	l.writer(fmt.Sprintf(format, a...))
	return nil
}

func main() {
	a := app.NewWithID("com.komari.agent")
	w := a.NewWindow("Komari Agent")

	// Preferences（从持久化存储中读取上次保存的配置）
	prefs := a.Preferences()
	savedEndpoint := prefs.StringWithFallback("endpoint", "")
	savedToken := prefs.StringWithFallback("token", "")
	savedTLS := prefs.BoolWithFallback("tls", false)

	// UI Elements（构建用户界面元素）
	endpointEntry := widget.NewEntry()
	endpointEntry.SetText(savedEndpoint)
	endpointEntry.SetPlaceHolder("https://panel.example.com")

	tokenEntry := widget.NewEntry()
	tokenEntry.SetText(savedToken)
	tokenEntry.SetPlaceHolder("Token (Client Secret)")

	tlsCheck := widget.NewCheck("Enable TLS (wss://)", nil)
	tlsCheck.SetChecked(savedTLS)

	scriptEntry := widget.NewMultiLineEntry()
	scriptEntry.SetPlaceHolder("Paste curl installation script...")
	scriptEntry.Wrapping = fyne.TextWrapWord

	statusLabel := widget.NewLabel("Status: Stopped")

	// 实时日志系统列表视图配置
	var logLines []string
	var logMutex sync.Mutex

	logList := widget.NewList(
		func() int {
			logMutex.Lock()
			defer logMutex.Unlock()
			return len(logLines)
		},
		func() fyne.CanvasObject {
			lbl := widget.NewLabel("Template log line")
			lbl.Wrapping = fyne.TextWrapWord
			return lbl
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			logMutex.Lock()
			defer logMutex.Unlock()
			if int(i) < len(logLines) {
				o.(*widget.Label).SetText(logLines[i])
			}
		},
	)

	appendLog := func(msg string) {
		logMutex.Lock()
		// 加前缀
		nowTxt := time.Now().Format("15:04:05")
		logLines = append(logLines, fmt.Sprintf("[%s] %s", nowTxt, msg))
		// 为了防止吃内存，最多保留 100 条
		if len(logLines) > 100 {
			logLines = logLines[len(logLines)-100:]
		}
		logMutex.Unlock()

		// 异步刷新 UI 并卷动到底部
		if logList != nil {
			logList.Refresh()
			logList.ScrollToBottom()
		}
	}

	guiLogObj := &GUILogger{writer: appendLog}
	// 将默认 Logger 初始化为指向 Fyne GUI
	logger.InitDefaultLogger(true, guiLogObj)

	var startStopBtn *widget.Button
	startStopBtn = widget.NewButton("Start Agent", func() {
		if isAgentRunning {
			// 停止 Agent：取消上下文
			if agentCancel != nil {
				agentCancel()
				agentCancel = nil
			}
			isAgentRunning = false
			startStopBtn.SetText("Start Agent")
			statusLabel.SetText("Status: Stopped")
			appendLog("Agent stopped by user action.")
		} else {
			// 输入校验：Endpoint 和 Token 不能为空
			if endpointEntry.Text == "" || tokenEntry.Text == "" {
				dialog.ShowError(
					errors.New("Endpoint and Token are required"),
					w,
				)
				return
			}

			// 保存配置到 Preferences
			prefs.SetString("endpoint", endpointEntry.Text)
			prefs.SetString("token", tokenEntry.Text)
			prefs.SetBool("tls", tlsCheck.Checked)

			deviceUUID := prefs.StringWithFallback("device_uuid", "")
			if deviceUUID == "" {
				var err error
				deviceUUID, err = uuid.GenerateUUID()
				if err != nil {
					dialog.ShowError(err, w)
					return
				}
				prefs.SetString("device_uuid", deviceUUID)
			}

			// 构建全局 Agent 配置
			agentConfig = &model.AgentConfig{
				Endpoint:          endpointEntry.Text,
				Token:             tokenEntry.Text,
				UUID:              deviceUUID,
				TLS:               tlsCheck.Checked,
				InsecureTLS:       true,
				DisableCommand:    true,  // 移动端默认禁用命令执行
				DisableAutoUpdate: true, // Android 端禁用自动更新
				DisableNAT:        true,  // 移动端默认禁用 NAT 穿透
				Interval:          1.0,
				InfoReportPeriod:  5,
				SkipConnCount:     true,  // Android 上连接数统计可能受权限限制
				SkipProcsCount:    true,  // Android 上进程数统计可能受权限限制
			}

			setEnv()
			monitor.InitConfig(agentConfig)
			initialized = false

			// 启动 Agent（携带可取消的上下文）
			ctx, cancel := context.WithCancel(context.Background())
			agentCancel = cancel
			go func() {
				// 保护 Agent 运行时的 panic，防止闪退
				defer func() {
					if r := recover(); r != nil {
						errMsg := fmt.Sprintf("Crashed: %v", r)
						statusLabel.SetText("Status: " + errMsg)
						appendLog(errMsg)
						isAgentRunning = false
						startStopBtn.SetText("Start Agent")
					}
				}()
				appendLog(fmt.Sprintf("Starting connection to %s...", agentConfig.Endpoint))
				run(ctx)
			}()

			isAgentRunning = true
			startStopBtn.SetText("Stop Agent")
			statusLabel.SetText("Status: Running")
		}
	})

	// "从脚本自动填充"按钮
	parseBtn := widget.NewButton("Auto Fill from Script", func() {
		script := scriptEntry.Text
		if script == "" {
			dialog.ShowInformation("Empty", "Please paste the curl/install script first.", w)
			return
		}

		// --endpoint VALUE / -e VALUE  / -e'VALUE' / --endpoint=VALUE
		reEndpoint := regexp.MustCompile(`(?:-e|--endpoint)\s+['"]?([\w\.:/@-]+)['"]?`)
		reEndpointEq := regexp.MustCompile(`(?:-e|--endpoint)=['"]?([\w\.:/@-]+)['"]?`)
		// --token VALUE / -t VALUE / -t'VALUE' / --token=VALUE
		reToken := regexp.MustCompile(`(?:-t|--token)\s+['"]?([\w\-]+)['"]?`)
		reTokenEq := regexp.MustCompile(`(?:-t|--token)=['"]?([\w\-]+)['"]?`)

		mEndpoint := reEndpoint.FindStringSubmatch(script)
		if len(mEndpoint) <= 1 {
			mEndpoint = reEndpointEq.FindStringSubmatch(script)
		}
		if len(mEndpoint) > 1 {
			endpointEntry.SetText(mEndpoint[1])
		}

		mToken := reToken.FindStringSubmatch(script)
		if len(mToken) <= 1 {
			mToken = reTokenEq.FindStringSubmatch(script)
		}
		if len(mToken) > 1 {
			tokenEntry.SetText(mToken[1])
		}

		dialog.ShowInformation("Success", "Fields populated from script", w)
	})

	// 构建界面布局
	configContainer := container.NewVBox(
		widget.NewLabelWithStyle("One-Click Setup", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		scriptEntry,
		parseBtn,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Manual Configuration", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Endpoint:"),
		endpointEntry,
		widget.NewLabel("Token:"),
		tokenEntry,
		tlsCheck,
	)

	// 日志区域（允许滚动占据其余空间）
	logPanel := container.NewVBox(
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Runtime Logs", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)

	logsScroll := container.NewBorder(nil, nil, nil, nil, logList)

	content := container.NewBorder(
		configContainer,                                    // 顶部表单
		container.NewVBox(statusLabel, startStopBtn),       // 底部按钮
		nil,                                                 // 左边
		nil,                                                 // 右边
		container.NewBorder(logPanel, nil, nil, nil, logsScroll), // 中部主要为日志
	)

	w.SetContent(container.NewPadded(content))
	w.Resize(fyne.NewSize(400, 750))
	w.ShowAndRun()
}
