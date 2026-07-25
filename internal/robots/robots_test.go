package robots

import (
	"testing"
	"time"

	"github.com/user/clone/internal/config"
)

func TestRobotsParser_AllowDisallow(t *testing.T) {
	parser := NewRobotsParser()
	parser.SetUserAgent("Go-WebCloner/1.0")

	entry := &RobotsEntry{
		Disallow: []string{"/admin/", "/private/"},
		Allow:    []string{"/admin/public/"},
	}

	tests := []struct {
		path      string
		expectOK  bool
	}{
		{"/page", true},
		{"/admin/", false},
		{"/admin/public/", true},
		{"/private/secret", false},
		{"/public/page", true},
	}

	cfg := &config.Config{
		RespectRobots: true,
		CrawlDelay:    1 * time.Second,
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			ok, _ := parser.evaluateRules(entry, tt.path, cfg)
			if ok != tt.expectOK {
				t.Errorf("evaluateRules(%s) = %v, want %v", tt.path, ok, tt.expectOK)
			}
		})
	}
}

func TestRobotsParser_WildcardMatch(t *testing.T) {
	parser := NewRobotsParser()

	tests := []struct {
		rule    string
		path    string
		expect  bool
	}{
		{"/*.jpg", "/image.jpg", true},
		{"/*.jpg", "/dir/image.jpg", true},
		{"/dir/*", "/dir/page", true},
		{"/dir/*", "/other/page", false},
		{"/dir/*/end", "/dir/anything/end", true},
		{"/dir/*/end", "/dir/anything/other", false},
	}

	for _, tt := range tests {
		t.Run(tt.rule+"_"+tt.path, func(t *testing.T) {
			result := parser.matchPath(tt.rule, tt.path)
			if result != tt.expect {
				t.Errorf("matchPath(%s, %s) = %v, want %v", tt.rule, tt.path, result, tt.expect)
			}
		})
	}
}

func TestRobotsParser_UserAgentMatch(t *testing.T) {
	parser := NewRobotsParser()

	tests := []struct {
		pattern string
		ua      string
		expect  bool
	}{
		{"*", "Go-WebCloner/1.0", true},
		{"Go-WebCloner", "Go-WebCloner/1.0", true},
		{"Googlebot", "Go-WebCloner/1.0", false},
		{"*", "AnyUA/1.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.ua, func(t *testing.T) {
			parser.userAgent = tt.ua
			result := parser.matchUserAgent(tt.pattern)
			if result != tt.expect {
				t.Errorf("matchUserAgent(%s, %s) = %v, want %v", tt.pattern, tt.ua, result, tt.expect)
			}
		})
	}
}

func TestRobotsParser_LongestMatchWins(t *testing.T) {
	parser := NewRobotsParser()

	entry := &RobotsEntry{
		Disallow: []string{"/page"},
		Allow:    []string{"/page/public"},
	}

	cfg := &config.Config{
		RespectRobots: true,
		CrawlDelay:    1 * time.Second,
	}

	ok, _ := parser.evaluateRules(entry, "/page/public/file", cfg)
	if !ok {
		t.Error("expected /page/public/file to be allowed (longer Allow rule wins)")
	}

	ok, _ = parser.evaluateRules(entry, "/page/other", cfg)
	if ok {
		t.Error("expected /page/other to be blocked (Disallow rule wins)")
	}
}
