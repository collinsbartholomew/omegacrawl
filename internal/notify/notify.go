package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"strings"
	"text/template"
	"time"

	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

// Config holds the notification channel configuration.
type Config struct {
	WebhookURL string      `json:"webhook_url"`
	SlackURL   string      `json:"slack_url"`
	SMTP       *SMTPConfig `json:"smtp"`
}

// SMTPConfig holds the SMTP server and recipient settings for email notifications.
type SMTPConfig struct {
	Host string   `json:"host"`
	Port int      `json:"port"`
	User string   `json:"user"`
	Pass string   `json:"pass"`
	From string   `json:"from"`
	To   []string `json:"to"`
}

// Notification is a single notification message with optional crawl metadata.
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

// Notifier delivers notifications to the configured channels.
type Notifier struct {
	cfg    *Config
	client *http.Client
}

// New creates a Notifier with an HTTP client using the given config.
func New(cfg *Config) *Notifier {
	return &Notifier{
		cfg:    cfg,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// Send delivers the notification to all configured channels and returns any errors.
func (n *Notifier) Send(nt Notification) error {
	if n == nil || n.cfg == nil {
		return nil
	}
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
	body, err := json.Marshal(nt)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook body: %w", err)
	}
	resp, err := n.client.Post(n.cfg.WebhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// A non-2xx response means the receiver rejected the notification;
	// silently treating it as delivered hides outages.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if _, copyErr := io.Copy(io.Discard, resp.Body); copyErr != nil {
			util.LogDebug("failed to discard webhook response body", zap.Error(copyErr))
		}
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	if _, copyErr := io.Copy(io.Discard, resp.Body); copyErr != nil {
		util.LogDebug("failed to discard webhook response body", zap.Error(copyErr))
	}
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
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal slack payload: %w", err)
	}
	resp, err := n.client.Post(n.cfg.SlackURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if _, copyErr := io.Copy(io.Discard, resp.Body); copyErr != nil {
			util.LogDebug("failed to discard slack response body", zap.Error(copyErr))
		}
		return fmt.Errorf("slack returned status %d", resp.StatusCode)
	}
	if _, copyErr := io.Copy(io.Discard, resp.Body); copyErr != nil {
		util.LogDebug("failed to discard slack response body", zap.Error(copyErr))
	}
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
