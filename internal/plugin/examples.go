package plugin

import (
	"context"
)

// ExampleEnricherPlugin is a simple example of a native Go enricher plugin
type ExampleEnricherPlugin struct {
	name string
}

func NewExampleEnricherPlugin() *ExampleEnricherPlugin {
	return &ExampleEnricherPlugin{name: "example-enricher"}
}

func (p *ExampleEnricherPlugin) Name() string {
	return p.name
}

func (p *ExampleEnricherPlugin) Type() PluginType {
	return PluginTypeEnricher
}

func (p *ExampleEnricherPlugin) Init(config map[string]string) error {
	return nil
}

func (p *ExampleEnricherPlugin) Execute(ctx context.Context, input *PluginContext) (*PluginResult, error) {
	// Add some enrichment data
	return &PluginResult{
		Success: true,
		Data: map[string]interface{}{
			"enriched_by":    p.name,
			"enrichment_time": "now",
		},
	}, nil
}

func (p *ExampleEnricherPlugin) Cleanup() error {
	return nil
}

// ExampleFilterPlugin filters URLs
type ExampleFilterPlugin struct {
	name string
}

func NewExampleFilterPlugin() *ExampleFilterPlugin {
	return &ExampleFilterPlugin{name: "example-filter"}
}

func (p *ExampleFilterPlugin) Name() string {
	return p.name
}

func (p *ExampleFilterPlugin) Type() PluginType {
	return PluginTypeFilter
}

func (p *ExampleFilterPlugin) Init(config map[string]string) error {
	return nil
}

func (p *ExampleFilterPlugin) Execute(ctx context.Context, input *PluginContext) (*PluginResult, error) {
	// Filter out URLs containing "admin" or "login"
	urls := []string{}
	if u, ok := input.Config["urls"]; ok {
		// Parse comma-separated URLs
		for _, url := range splitURLs(u) {
			if !contains(url, "admin") && !contains(url, "login") {
				urls = append(urls, url)
			}
		}
	}
	return &PluginResult{
		Success:       true,
		FilteredURLs:  urls,
	}, nil
}

func (p *ExampleFilterPlugin) Cleanup() error {
	return nil
}

func splitURLs(s string) []string {
	var result []string
	var current string
	for _, c := range s {
		if c == ',' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}