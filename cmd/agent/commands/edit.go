package commands

import (
	"fmt"

	"github.com/AlecAivazis/survey/v2"

	"github.com/komari-monitor/komari-agent/model"
)

func EditAgentConfig(configPath string, agentConfig *model.AgentConfig) {
	cfg, err := model.LoadConfig(configPath)
	if err != nil {
		cfg = model.DefaultConfig()
	}

	var qs = []*survey.Question{
		{
			Name: "endpoint",
			Prompt: &survey.Input{
				Message: "服务器地址",
				Default: cfg.Endpoint,
			},
		},
		{
			Name: "token",
			Prompt: &survey.Input{
				Message: "客户端 Token",
				Default: cfg.Token,
			},
		},
		{
			Name: "interval",
			Prompt: &survey.Input{
				Message: "上报间隔（秒）",
				Default: fmt.Sprintf("%.1f", cfg.Interval),
			},
		},
	}

	answers := struct {
		Endpoint string
		Token    string
		Interval string
	}{}

	err = survey.Ask(qs, &answers, survey.WithValidator(survey.Required))
	if err != nil {
		fmt.Println("选择错误", err.Error())
		return
	}

	cfg.Endpoint = answers.Endpoint
	cfg.Token = answers.Token
	if answers.Interval != "" {
		var interval float64
		if _, err := fmt.Sscanf(answers.Interval, "%f", &interval); err == nil {
			cfg.Interval = interval
		}
	}

	if err := cfg.Save(); err != nil {
		panic(err)
	}
	fmt.Println("修改配置成功，重启 Agent 后生效")
}
