//go:build gui || android

package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
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
	isAgentRunning bool
	agentCancel    context.CancelFunc
)

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

	prefs := a.Preferences()
	savedEndpoint := prefs.StringWithFallback("endpoint", "")
	savedToken := prefs.StringWithFallback("token", "")
	savedTLS := prefs.BoolWithFallback("tls", false)

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
		nowTxt := time.Now().Format("15:04:05")
		logLines = append(logLines, fmt.Sprintf("[%s] %s", nowTxt, msg))
		if len(logLines) > 100 {
			logLines = logLines[len(logLines)-100:]
		}
		logMutex.Unlock()

		if logList != nil {
			logList.Refresh()
			logList.ScrollToBottom()
		}
	}

	guiLogObj := &GUILogger{writer: appendLog}
	logger.InitDefaultLogger(true, guiLogObj)

	var startStopBtn *widget.Button
	startStopBtn = widget.NewButton("Start Agent", func() {
		if isAgentRunning {
			if agentCancel != nil {
				agentCancel()
				agentCancel = nil
			}
			isAgentRunning = false
			startStopBtn.SetText("Start Agent")
			statusLabel.SetText("Status: Stopped")
			appendLog("Agent stopped by user action.")
		} else {
			if endpointEntry.Text == "" || tokenEntry.Text == "" {
				dialog.ShowError(
					errors.New("Endpoint and Token are required"),
					w,
				)
				return
			}

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

			agentConfig = &model.AgentConfig{
				Endpoint:          endpointEntry.Text,
				Token:             tokenEntry.Text,
				UUID:              deviceUUID,
				TLS:               tlsCheck.Checked,
				InsecureTLS:       true,
				DisableCommand:    true,
				DisableAutoUpdate: true,
				DisableNAT:        true,
				Interval:          1.0,
				InfoReportPeriod:  5,
				SkipConnCount:     true,
				SkipProcsCount:    true,
			}

			setEnv()
			monitor.InitConfig(agentConfig)
			initialized = false

			ctx, cancel := context.WithCancel(context.Background())
			agentCancel = cancel
			go func() {
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

	logPanel := container.NewVBox(
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Runtime Logs", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)

	logsScroll := container.NewBorder(nil, nil, nil, nil, logList)

	content := container.NewBorder(
		configContainer,
		container.NewVBox(statusLabel, startStopBtn),
		nil,
		nil,
		container.NewBorder(logPanel, nil, nil, nil, logsScroll),
	)

	w.SetContent(container.NewPadded(content))
	w.Resize(fyne.NewSize(400, 750))
	w.ShowAndRun()
}
