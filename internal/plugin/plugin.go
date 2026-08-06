package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/user/clone/internal/tracing"
)

// PluginType represents the type of plugin
type PluginType string

const (
	// PluginTypeExtractor extracts content from pages
	PluginTypeExtractor PluginType = "extractor"
	// PluginTypeTransformer transforms page content
	PluginTypeTransformer PluginType = "transformer"
	// PluginTypeEnricher enriches page data with external data
	PluginTypeEnricher PluginType = "enricher"
	// PluginTypeFilter filters pages/URLs
	PluginTypeFilter PluginType = "filter"
)

// PluginManifest describes a plugin
type PluginManifest struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Type        PluginType        `json:"type"`
	Description string            `json:"description"`
	Author      string            `json:"author"`
	EntryPoint  string            `json:"entry_point"` // WASM export name
	Config      map[string]string `json:"config,omitempty"`
}

// PluginContext provides context to plugins
type PluginContext struct {
	URL         string                 `json:"url"`
	Depth       int                    `json:"depth"`
	HTML        string                 `json:"html"`
	Metadata    map[string]interface{} `json:"metadata"`
	Config      map[string]string      `json:"config"`
}

// PluginResult represents the result of plugin execution
type PluginResult struct {
	Success   bool                   `json:"success"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Error     string                 `json:"error,omitempty"`
	TransformedHTML string           `json:"transformed_html,omitempty"`
	FilteredURLs    []string         `json:"filtered_urls,omitempty"`
}

// Plugin is the interface all plugins must implement
type Plugin interface {
	// Name returns the plugin name
	Name() string
	// Type returns the plugin type
	Type() PluginType
	// Init initializes the plugin with configuration
	Init(config map[string]string) error
	// Execute runs the plugin logic
	Execute(ctx context.Context, input *PluginContext) (*PluginResult, error)
	// Cleanup releases resources
	Cleanup() error
}

// WASMPlugin wraps a WebAssembly module as a plugin
type WASMPlugin struct {
	name        string
	pluginType  PluginType
	manifest    *PluginManifest
	module      wazero.CompiledModule
	runtime     wazero.Runtime
	instance    api.Module
	mu          sync.Mutex
	config      map[string]string
}

// PluginManager manages plugin lifecycle
type PluginManager struct {
	plugins     map[string]Plugin
	wasmRuntime wazero.Runtime
	mu          sync.RWMutex
	pluginDir   string
}

// NewPluginManager creates a new plugin manager
func NewPluginManager(pluginDir string) (*PluginManager, error) {
	ctx := context.Background()
	
	// Create WASM runtime with WASI support
	r := wazero.NewRuntime(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, r)
	
	return &PluginManager{
		plugins:     make(map[string]Plugin),
		wasmRuntime: r,
		pluginDir:   pluginDir,
	}, nil
}

// LoadPlugin loads a plugin from a WASM file
func (pm *PluginManager) LoadPlugin(ctx context.Context, wasmPath string) error {
	// Read manifest
	manifestPath := wasmPath[:len(wasmPath)-5] + ".json" // .wasm -> .json
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest PluginManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	// Read WASM module
	wasmData, err := os.ReadFile(wasmPath)
	if err != nil {
		return fmt.Errorf("failed to read WASM: %w", err)
	}

	// Compile module
	compiled, err := pm.wasmRuntime.CompileModule(ctx, wasmData)
	if err != nil {
		return fmt.Errorf("failed to compile WASM: %w", err)
	}

	// Instantiate module
	config := wazero.NewModuleConfig().
		WithStdout(os.Stdout).
		WithStderr(os.Stderr).
		WithArgs(manifest.EntryPoint)

	instance, err := pm.wasmRuntime.InstantiateModule(ctx, compiled, config)
	if err != nil {
		return fmt.Errorf("failed to instantiate WASM: %w", err)
	}

	plugin := &WASMPlugin{
		name:       manifest.Name,
		pluginType: manifest.Type,
		manifest:   &manifest,
		module:     compiled,
		instance:   instance,
		runtime:    pm.wasmRuntime,
		config:     manifest.Config,
	}

	pm.mu.Lock()
	pm.plugins[manifest.Name] = plugin
	pm.mu.Unlock()

	return plugin.Init(manifest.Config)
}

// LoadPluginsFromDir loads all plugins from a directory
func (pm *PluginManager) LoadPluginsFromDir(ctx context.Context) error {
	entries, err := os.ReadDir(pm.pluginDir)
	if err != nil {
		return fmt.Errorf("failed to read plugin dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) == ".wasm" {
			wasmPath := filepath.Join(pm.pluginDir, entry.Name())
			if err := pm.LoadPlugin(ctx, wasmPath); err != nil {
				return fmt.Errorf("failed to load plugin %s: %w", entry.Name(), err)
			}
		}
	}
	return nil
}

// GetPlugin returns a plugin by name
func (pm *PluginManager) GetPlugin(name string) (Plugin, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	p, ok := pm.plugins[name]
	return p, ok
}

// GetPluginsByType returns all plugins of a given type
func (pm *PluginManager) GetPluginsByType(pluginType PluginType) []Plugin {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	
	var result []Plugin
	for _, p := range pm.plugins {
		if p.Type() == pluginType {
			result = append(result, p)
		}
	}
	return result
}

// ExecuteExtractorPlugins runs all extractor plugins on a page
func (pm *PluginManager) ExecuteExtractorPlugins(ctx context.Context, input *PluginContext) ([]*PluginResult, error) {
	plugins := pm.GetPluginsByType(PluginTypeExtractor)
	results := make([]*PluginResult, 0, len(plugins))

	for _, p := range plugins {
		ctx, span := tracing.StartSpan(ctx, "plugin.execute",
			tracing.WithAttribute("plugin", p.Name()),
			tracing.WithAttribute("type", string(p.Type())),
		)
		
		result, err := p.Execute(ctx, input)
		if err != nil {
			span.RecordError(err)
			results = append(results, &PluginResult{
				Success: false,
				Error:   err.Error(),
			})
		} else {
			results = append(results, result)
		}
		span.End()
	}

	return results, nil
}

// ExecuteTransformerPlugins runs all transformer plugins on a page
func (pm *PluginManager) ExecuteTransformerPlugins(ctx context.Context, input *PluginContext) (string, error) {
	plugins := pm.GetPluginsByType(PluginTypeTransformer)
	html := input.HTML

	for _, p := range plugins {
		ctx, span := tracing.StartSpan(ctx, "plugin.transform",
			tracing.WithAttribute("plugin", p.Name()),
		)
		
		input.HTML = html
		result, err := p.Execute(ctx, input)
		if err != nil {
			span.RecordError(err)
			return "", err
		}
		if result.TransformedHTML != "" {
			html = result.TransformedHTML
		}
		span.End()
	}

	return html, nil
}

// ExecuteFilterPlugins runs all filter plugins on URLs
func (pm *PluginManager) ExecuteFilterPlugins(ctx context.Context, urls []string) ([]string, error) {
	plugins := pm.GetPluginsByType(PluginTypeFilter)
	filtered := urls

	for _, p := range plugins {
		ctx, span := tracing.StartSpan(ctx, "plugin.filter",
			tracing.WithAttribute("plugin", p.Name()),
		)
		
		input := &PluginContext{
			URL:    filtered[0], // Use first URL as context
			Config: map[string]string{"urls": joinURLs(filtered)},
		}
		
		result, err := p.Execute(ctx, input)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		if len(result.FilteredURLs) > 0 {
			filtered = result.FilteredURLs
		}
		span.End()
	}

	return filtered, nil
}

// ExecuteEnricherPlugins runs all enricher plugins
func (pm *PluginManager) ExecuteEnricherPlugins(ctx context.Context, input *PluginContext) (map[string]interface{}, error) {
	plugins := pm.GetPluginsByType(PluginTypeEnricher)
	enriched := make(map[string]interface{})

	for _, p := range plugins {
		ctx, span := tracing.StartSpan(ctx, "plugin.enrich",
			tracing.WithAttribute("plugin", p.Name()),
		)
		
		result, err := p.Execute(ctx, input)
		if err != nil {
			span.RecordError(err)
			continue
		}
		for k, v := range result.Data {
			enriched[k] = v
		}
		span.End()
	}

	return enriched, nil
}

// Close shuts down the plugin manager
func (pm *PluginManager) Close(ctx context.Context) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for _, p := range pm.plugins {
		if err := p.Cleanup(); err != nil {
			// Log but continue
		}
	}

	return pm.wasmRuntime.Close(ctx)
}

// WASMPlugin implementation
func (wp *WASMPlugin) Name() string {
	return wp.name
}

func (wp *WASMPlugin) Type() PluginType {
	return wp.pluginType
}

func (wp *WASMPlugin) Init(config map[string]string) error {
	wp.mu.Lock()
	defer wp.mu.Unlock()
	wp.config = config
	return nil
}

func (wp *WASMPlugin) Execute(ctx context.Context, input *PluginContext) (*PluginResult, error) {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	// Serialize input to JSON (for future WASM memory write)
	_, err := json.Marshal(input)
	if err != nil {
		return &PluginResult{Success: false, Error: err.Error()}, nil
	}

	// Call WASM export function
	// This is a simplified version - actual implementation would use
	// WASM memory for passing data
	fn := wp.instance.ExportedFunction(wp.manifest.EntryPoint)
	if fn == nil {
		return &PluginResult{Success: false, Error: "entry point not found"}, nil
	}

	// In a real implementation, we would:
	// 1. Write input to WASM memory
	// 2. Call the function
	// 3. Read result from WASM memory
	// For now, return mock result
	
	return &PluginResult{
		Success: true,
		Data:    map[string]interface{}{"processed": true},
	}, nil
}

func (wp *WASMPlugin) Cleanup() error {
	wp.mu.Lock()
	defer wp.mu.Unlock()
	
	if wp.instance != nil {
		wp.instance.Close(context.Background())
	}
	return nil
}

// GoPlugin is a native Go plugin (for development/testing)
type GoPlugin struct {
	plugin Plugin
}

func (gp *GoPlugin) Name() string {
	return gp.plugin.Name()
}

func (gp *GoPlugin) Type() PluginType {
	return gp.plugin.Type()
}

func (gp *GoPlugin) Init(config map[string]string) error {
	return gp.plugin.Init(config)
}

func (gp *GoPlugin) Execute(ctx context.Context, input *PluginContext) (*PluginResult, error) {
	return gp.plugin.Execute(ctx, input)
}

func (gp *GoPlugin) Cleanup() error {
	return gp.plugin.Cleanup()
}

// RegisterGoPlugin registers a native Go plugin
func (pm *PluginManager) RegisterGoPlugin(p Plugin) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	
	if _, exists := pm.plugins[p.Name()]; exists {
		return fmt.Errorf("plugin %s already registered", p.Name())
	}
	
	pm.plugins[p.Name()] = p
	return p.Init(nil)
}

// Helper function
func joinURLs(urls []string) string {
	result := ""
	for i, u := range urls {
		if i > 0 {
			result += ","
		}
		result += u
	}
	return result
}

// BuiltinExtractorPlugin is an example built-in extractor
type BuiltinExtractorPlugin struct {
	name string
}

func NewBuiltinExtractorPlugin(name string) *BuiltinExtractorPlugin {
	return &BuiltinExtractorPlugin{name: name}
}

func (p *BuiltinExtractorPlugin) Name() string {
	return p.name
}

func (p *BuiltinExtractorPlugin) Type() PluginType {
	return PluginTypeExtractor
}

func (p *BuiltinExtractorPlugin) Init(config map[string]string) error {
	return nil
}

func (p *BuiltinExtractorPlugin) Execute(ctx context.Context, input *PluginContext) (*PluginResult, error) {
	// Example: Extract word count
	wordCount := 0
	inWord := false
	for _, r := range input.HTML {
		if r == ' ' || r == '\n' || r == '\t' {
			inWord = false
		} else if !inWord {
			wordCount++
			inWord = true
		}
	}

	return &PluginResult{
		Success: true,
		Data: map[string]interface{}{
			"word_count": wordCount,
			"char_count": len(input.HTML),
		},
	}, nil
}

func (p *BuiltinExtractorPlugin) Cleanup() error {
	return nil
}