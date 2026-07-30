package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"
	"text/template"
	"time"
)

type Config struct {
	WebhookURL string `json:"webhook_url"`
	SlackURL   string `json:"slack_url"`
	SMTP       *SMTPConfig `json:"smtp"`
}

type SMTPConfig struct {
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	User     string   `json:"user"`
	Pass     string   `json:"pass"`
	From     string   `json:"from"`
	To       []string `json:"to"`
}

type Notification struct {
	Title    string `json:"title"`
	Message  string `json:"message"`
	Level    string `json:"level"`
	URL      string `json:"url,omitempty"`
	Duration string `json:"duration,omitempty"`
	Pages    int    `json:"pages,omitempty"`
	Errors   int    `json:"errors,omitempty"`
	Bytes    int64  `json:"bytes,omitempty"`
}

type Notifier struct {
	cfg    *Config
	client *http.Client
}

func New(cfg *Config) *Notifier {
	return &Notifier{
		cfg:    cfg,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (n *Notifier) Send(nt Notification) error {
	var errs []string
	if n.cfg.WebhookURL != "" {
		if err := n.sendWebhook(nt); err != nil {
			errs = append(errs, fmt.Sprintf("webhook: %v", err))
		}
	}
	if n.cfg.SlackURL != "" {
		if err := n.sendSlack(nt); err != nil {
			errs = append(errs, fmt.Sprintf("slack: %v", err))
		}
	}
	if n.cfg.SMTP != nil {
		if err := n.sendEmail(nt); err != nil {
			errs = append(errs, fmt.Sprintf("email: %v", err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("notify errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (n *Notifier) sendWebhook(nt Notification) error {
	body, _ := json.Marshal(nt)
	resp, err := n.client.Post(n.cfg.WebhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (n *Notifier) sendSlack(nt Notification) error {
	color := "#36a64f"
	if nt.Level == "error" || nt.Level == "critical" {
		color = "#ff0000"
	} else if nt.Level == "warning" {
		color = "#ffcc00"
	}
	payload := map[string]interface{}{
		"attachments": []map[string]interface{}{
			{
				"color":  color,
				"title":  nt.Title,
				"text":   nt.Message,
				"fields": slackFields(nt),
				"ts":     time.Now().Unix(),
			},
		},
	}
	body, _ := json.Marshal(payload)
	resp, err := n.client.Post(n.cfg.SlackURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func slackFields(nt Notification) []map[string]interface{} {
	var fields []map[string]interface{}
	if nt.URL != "" {
		fields = append(fields, map[string]interface{}{"title": "URL", "value": nt.URL, "short": true})
	}
	if nt.Duration != "" {
		fields = append(fields, map[string]interface{}{"title": "Duration", "value": nt.Duration, "short": true})
	}
	if nt.Pages > 0 {
		fields = append(fields, map[string]interface{}{"title": "Pages", "value": fmt.Sprintf("%d", nt.Pages), "short": true})
	}
	if nt.Errors > 0 {
		fields = append(fields, map[string]interface{}{"title": "Errors", "value": fmt.Sprintf("%d", nt.Errors), "short": true})
	}
	return fields
}

func (n *Notifier) sendEmail(nt Notification) error {
	cfg := n.cfg.SMTP
	if cfg == nil || len(cfg.To) == 0 {
		return nil
	}
	tpl := template.Must(template.New("email").Parse(emailTemplate))
	var body bytes.Buffer
	tpl.Execute(&body, nt)

	headers := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n",
		cfg.From, strings.Join(cfg.To, ","), nt.Title)

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	auth := smtp.PlainAuth("", cfg.User, cfg.Pass, cfg.Host)
	msg := []byte(headers + body.String())
	return smtp.SendMail(addr, auth, cfg.From, cfg.To, msg)
}

var emailTemplate = `
<html><body style="font-family:sans-serif;padding:20px;">
<h2>{{.Title}}</h2>
<p>{{.Message}}</p>
<table style="border-collapse:collapse;">
{{if .URL}}<tr><td style="padding:4px 8px;font-weight:bold;">URL</td><td>{{.URL}}</td></tr>{{end}}
{{if .Duration}}<tr><td style="padding:4px 8px;font-weight:bold;">Duration</td><td>{{.Duration}}</td></tr>{{end}}
{{if .Pages}}<tr><td style="padding:4px 8px;font-weight:bold;">Pages</td><td>{{.Pages}}</td></tr>{{end}}
{{if .Errors}}<tr><td style="padding:4px 8px;font-weight:bold;">Errors</td><td>{{.Errors}}</td></tr>{{end}}
</table>
</body></html>
`
