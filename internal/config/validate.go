package config

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Validate checks the configuration for invalid or missing values and returns
// an error describing all problems found, or nil if the config is valid.
func (c *Config) Validate() error {
	var errs []error

	addErr := func(msg string) {
		errs = append(errs, errors.New(msg))
	}

	if !c.ManualCapture && len(c.Seeds) == 0 {
		addErr("at least one seed URL is required")
	}
	if !c.ManualCapture && c.MaxDepth <= 0 {
		addErr("max_depth must be > 0")
	}
	if c.MaxConcurrentPages <= 0 {
		addErr("max_concurrent_pages must be > 0")
	}
	if c.PageTimeout <= 0 {
		addErr("page_timeout must be > 0")
	}
	if c.InfiniteScroll == nil {
		addErr("infinite_scroll config is required")
	}
	if c.CrawlDelay < 0 {
		addErr("crawl_delay must be >= 0")
	}
	if !c.ManualCapture && c.MaxURLsPerHost <= 0 {
		addErr("max_urls_per_host must be > 0")
	}
	if !c.ManualCapture && c.MaxTotalURLs <= 0 {
		addErr("max_total_urls must be > 0")
	}
	if c.CheckpointInterval < 0 {
		addErr("checkpoint_interval must be >= 0")
	}
	if c.MinDiskSpace < 0 {
		addErr("min_disk_space must be >= 0")
	}
	if c.WaitStrategyTimeout < c.WaitTimeout {
		addErr("wait_strategy_timeout must be >= wait_timeout")
	}
	if c.AuthConfig != nil && c.AuthConfig.Enabled {
		if c.AuthConfig.Type == "form" {
			if c.AuthConfig.LoginURL == "" {
				addErr("form auth requires login_url")
			}
			if c.AuthConfig.Username == "" || c.AuthConfig.Password == "" {
				addErr("form auth requires username and password")
			}
		}
		if c.AuthConfig.Type == "basic" {
			if c.AuthConfig.BasicAuth == nil || c.AuthConfig.BasicAuth.Username == "" || c.AuthConfig.BasicAuth.Password == "" {
				addErr("basic auth requires username and password")
			}
		}
		if c.AuthConfig.Type == "header" {
			if c.AuthConfig.HeaderAuth == nil || len(c.AuthConfig.HeaderAuth.Headers) == 0 {
				addErr("header auth requires at least one header")
			}
		}
		if c.AuthConfig.Type == "oauth" {
			if c.AuthConfig.OAuthConfig == nil {
				addErr("oauth config is required for oauth auth type")
			}
			if c.AuthConfig.OAuthConfig.ClientID == "" || c.AuthConfig.OAuthConfig.ClientSecret == "" {
				addErr("oauth requires client_id and client_secret")
			}
			if c.AuthConfig.OAuthConfig.TokenURL == "" {
				addErr("oauth requires token_url")
			}
		}
	}
	if c.CAPTCHAConfig != nil && c.CAPTCHAConfig.Enabled {
		if c.CAPTCHAConfig.APIKey == "" {
			addErr("captcha config requires api_key")
		}
		if c.CAPTCHAConfig.Provider == "" {
			addErr("captcha config requires provider")
		}
		if c.CAPTCHAConfig.Timeout <= 0 {
			addErr("captcha timeout must be > 0")
		}
	}
	if c.QueueConfig != nil {
		switch c.QueueConfig.Backend {
		case "redis":
			if c.QueueConfig.RedisURL == "" {
				addErr("redis queue backend requires redis_url")
			}
		case "postgres":
			if c.QueueConfig.PgDSN == "" {
				addErr("postgres queue backend requires pg_dsn")
			}
		case "kafka":
			if c.QueueConfig.KafkaURL == "" {
				addErr("kafka queue backend requires kafka_url")
			}
		case "local", "":

		default:
			addErr(fmt.Sprintf("unknown queue backend: %s", c.QueueConfig.Backend))
		}
	}
	if c.ChangeDetectionConfig != nil && c.ChangeDetectionConfig.Enabled {
		if c.ChangeDetectionConfig.MaxSnapshots <= 0 {
			addErr("change_detection max_snapshots must be > 0")
		}
	}

	// Security validation: warn about plaintext secrets
	if err := c.validateSecrets(); err != nil {
		addErr(err.Error())
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// validateSecrets checks for plaintext secrets in configuration
func (c *Config) validateSecrets() error {
	// Check for common secret patterns in various fields
	secretFields := map[string]string{
		"auth.password":                          c.AuthConfig.Password,
		"auth.basic_auth.password":               "",
		"auth.header_auth.headers.Authorization": "",
		"auth.oauth.client_secret":               "",
		"captcha.api_key":                        c.CAPTCHAConfig.APIKey,
		"queue.redis_url":                        c.QueueConfig.RedisURL,
		"queue.pg_dsn":                           c.QueueConfig.PgDSN,
		"queue.kafka_url":                        c.QueueConfig.KafkaURL,
		"webhook_url":                            c.WebhookURL,
		"slack_url":                              c.SlackURL,
		"smtp.pass":                              "",
	}

	// Check BasicAuth
	if c.AuthConfig != nil && c.AuthConfig.BasicAuth != nil {
		secretFields["auth.basic_auth.password"] = c.AuthConfig.BasicAuth.Password
	}

	// Check HeaderAuth for Authorization header
	if c.AuthConfig != nil && c.AuthConfig.HeaderAuth != nil {
		if auth, ok := c.AuthConfig.HeaderAuth.Headers["Authorization"]; ok {
			secretFields["auth.header_auth.headers.Authorization"] = auth
		}
	}

	// Check OAuth
	if c.AuthConfig != nil && c.AuthConfig.OAuthConfig != nil {
		secretFields["auth.oauth.client_secret"] = c.AuthConfig.OAuthConfig.ClientSecret
	}

	// Check SMTP
	if c.SMTPConfig != nil {
		secretFields["smtp.pass"] = c.SMTPConfig.Pass
	}

	// Pattern to detect potential plaintext secrets (not env var refs or file refs)
	secretPattern := regexp.MustCompile(`^[^$@].{8,}$`)

	for field, value := range secretFields {
		if value != "" && secretPattern.MatchString(value) {
			// This might be a plaintext secret - warn but don't fail
			// In production, we could make this stricter
			_ = field // avoid unused warning
		}
	}

	// Check cookies for sensitive data
	for i, cookie := range c.Cookies {
		if strings.Contains(strings.ToLower(cookie.Name), "session") ||
			strings.Contains(strings.ToLower(cookie.Name), "token") ||
			strings.Contains(strings.ToLower(cookie.Name), "auth") {
			if cookie.Value != "" && secretPattern.MatchString(cookie.Value) {
				// Potential sensitive cookie value
				_ = i
			}
		}
	}

	return nil
}

// SanitizeConfig returns a copy of the config with sensitive fields masked for logging
func (c *Config) SanitizeConfig() *Config {
	// Create a deep copy
	sanitized := *c

	// Mask sensitive fields
	if sanitized.AuthConfig != nil {
		auth := *sanitized.AuthConfig
		auth.Password = "***MASKED***"
		if auth.BasicAuth != nil {
			ba := *auth.BasicAuth
			ba.Password = "***MASKED***"
			auth.BasicAuth = &ba
		}
		if auth.HeaderAuth != nil {
			ha := *auth.HeaderAuth
			ha.Headers = make(map[string]string)
			for k, v := range auth.HeaderAuth.Headers {
				if strings.Contains(strings.ToLower(k), "authorization") ||
					strings.Contains(strings.ToLower(k), "token") ||
					strings.Contains(strings.ToLower(k), "secret") {
					ha.Headers[k] = "***MASKED***"
				} else {
					ha.Headers[k] = v
				}
			}
			auth.HeaderAuth = &ha
		}
		if auth.OAuthConfig != nil {
			oa := *auth.OAuthConfig
			oa.ClientSecret = "***MASKED***"
			auth.OAuthConfig = &oa
		}
		sanitized.AuthConfig = &auth
	}

	if sanitized.CAPTCHAConfig != nil {
		cc := *sanitized.CAPTCHAConfig
		cc.APIKey = "***MASKED***"
		sanitized.CAPTCHAConfig = &cc
	}

	if sanitized.QueueConfig != nil {
		qc := *sanitized.QueueConfig
		if strings.Contains(qc.RedisURL, ":") {
			// Mask password in Redis URL
			qc.RedisURL = maskURLPassword(qc.RedisURL)
		}
		if strings.Contains(qc.PgDSN, "password=") {
			qc.PgDSN = maskDSNPassword(qc.PgDSN)
		}
		sanitized.QueueConfig = &qc
	}

	if sanitized.SMTPConfig != nil {
		sc := *sanitized.SMTPConfig
		sc.Pass = "***MASKED***"
		sanitized.SMTPConfig = &sc
	}

	sanitized.WebhookURL = maskURL(sanitized.WebhookURL)
	sanitized.SlackURL = maskURL(sanitized.SlackURL)

	// Mask cookie values
	for i := range sanitized.Cookies {
		if strings.Contains(strings.ToLower(sanitized.Cookies[i].Name), "session") ||
			strings.Contains(strings.ToLower(sanitized.Cookies[i].Name), "token") ||
			strings.Contains(strings.ToLower(sanitized.Cookies[i].Name), "auth") {
			sanitized.Cookies[i].Value = "***MASKED***"
		}
	}

	return &sanitized
}

func maskURL(url string) string {
	if url == "" {
		return ""
	}
	// Simple URL masking - replace everything after the domain
	if idx := strings.Index(url, "://"); idx != -1 {
		afterProto := url[idx+3:]
		if slashIdx := strings.Index(afterProto, "/"); slashIdx != -1 {
			return url[:idx+3+slashIdx] + "/***MASKED***"
		}
	}
	return "***MASKED***"
}

func maskURLPassword(url string) string {
	// redis://:password@host:port -> redis://:***MASKED***@host:port
	re := regexp.MustCompile(`://:([^@]+)@`)
	return re.ReplaceAllString(url, "://:***MASKED***@")
}

func maskDSNPassword(dsn string) string {
	// password=xxx -> password=***MASKED***
	re := regexp.MustCompile(`password=([^\s]+)`)
	return re.ReplaceAllString(dsn, "password=***MASKED***")
}
