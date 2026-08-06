package jsengine

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStealthScriptAntiFingerprinting(t *testing.T) {
	checks := []struct {
		name    string
		pattern string
	}{
		{"canvas getImageData noise", `CanvasRenderingContext2D.prototype.getImageData`},
		{"canvas toDataURL noise", `HTMLCanvasElement.prototype.toDataURL`},
		{"webdriver hidden", `'webdriver'`},
		{"plugins mocked", `'plugins'`},
		{"webgl vendor masked", `37445`},
		{"hardwareConcurrency fixed", `'hardwareConcurrency'`},
		{"maxTouchPoints fixed", `'maxTouchPoints'`},
		{"screen size fixed", `screen, 'width'`},
	}
	for _, c := range checks {
		if !strings.Contains(StealthScript, c.pattern) {
			t.Errorf("StealthScript missing %s (pattern %q)", c.name, c.pattern)
		}
	}

	broken := []struct {
		name    string
		pattern string
	}{
		{"font check forced true", `fonts.check = function() { return true; }`},
		{"broken webrtc sdp regex", `c=IN IP4\\d`},
		{"broken webrtc ip6 regex", `c=IN IP6[^`},
	}
	for _, b := range broken {
		if strings.Contains(StealthScript, b.pattern) {
			t.Errorf("StealthScript still contains broken pattern %q (%s)", b.pattern, b.name)
		}
	}
}

func TestPushStateRouteJSON(t *testing.T) {
	jsonStr := `[{"url":"/about","type":"pushState","title":"About","timestamp":1000},{"url":"/contact","type":"replaceState","title":"","timestamp":2000}]`
	var routes []PushStateRoute
	if err := json.Unmarshal([]byte(jsonStr), &routes); err != nil {
		t.Fatalf("failed to unmarshal push state routes: %v", err)
	}
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(routes))
	}
	if routes[0].URL != "/about" {
		t.Errorf("expected /about, got %s", routes[0].URL)
	}
	if routes[0].Type != "pushState" {
		t.Errorf("expected pushState, got %s", routes[0].Type)
	}
	if routes[1].URL != "/contact" {
		t.Errorf("expected /contact, got %s", routes[1].URL)
	}
}

func TestIframeSourceJSON(t *testing.T) {
	jsonStr := `[{"src":"https://example.com/embed","width":"100%","height":"400","id":"myframe","sandbox":""}]`
	var iframes []IframeSource
	if err := json.Unmarshal([]byte(jsonStr), &iframes); err != nil {
		t.Fatalf("failed to unmarshal iframe sources: %v", err)
	}
	if len(iframes) != 1 {
		t.Fatalf("expected 1 iframe, got %d", len(iframes))
	}
	if iframes[0].Src != "https://example.com/embed" {
		t.Errorf("expected https://example.com/embed, got %s", iframes[0].Src)
	}
	if iframes[0].Width != "100%" {
		t.Errorf("expected 100%%, got %s", iframes[0].Width)
	}
}

func TestMediaSourceJSON(t *testing.T) {
	jsonStr := `[{"src":"https://example.com/video.mp4","type":"video","poster":"https://example.com/poster.jpg","width":"640","height":"480","id":"myvideo"}]`
	var media []MediaSource
	if err := json.Unmarshal([]byte(jsonStr), &media); err != nil {
		t.Fatalf("failed to unmarshal media sources: %v", err)
	}
	if len(media) != 1 {
		t.Fatalf("expected 1 media source, got %d", len(media))
	}
	if media[0].Src != "https://example.com/video.mp4" {
		t.Errorf("expected video URL, got %s", media[0].Src)
	}
	if media[0].Type != "video" {
		t.Errorf("expected video type, got %s", media[0].Type)
	}
	if media[0].Poster != "https://example.com/poster.jpg" {
		t.Errorf("expected poster URL, got %s", media[0].Poster)
	}
}

func TestStructuredDataJSON(t *testing.T) {
	jsonStr := `{"jsonld":[{"@context":"https://schema.org","@type":"WebSite"}],"og":{"title":"Test Page","description":"A test"},"twitter":{"card":"summary","site":"@test"},"meta":{"viewport":"width=device-width"}}`
	var sd StructuredData
	if err := json.Unmarshal([]byte(jsonStr), &sd); err != nil {
		t.Fatalf("failed to unmarshal structured data: %v", err)
	}
	if len(sd.JSONLD) != 1 {
		t.Errorf("expected 1 JSON-LD entry, got %d", len(sd.JSONLD))
	}
	if sd.OG["title"] != "Test Page" {
		t.Errorf("expected OG title 'Test Page', got %s", sd.OG["title"])
	}
	if sd.Twitter["card"] != "summary" {
		t.Errorf("expected Twitter card 'summary', got %s", sd.Twitter["card"])
	}
	if sd.Meta["viewport"] != "width=device-width" {
		t.Errorf("expected viewport 'width=device-width', got %s", sd.Meta["viewport"])
	}
}
