package jsanalyzer

import (
	"sort"
	"testing"
)

func TestExtractJSURLs(t *testing.T) {
	tests := []struct {
		name     string
		js       string
		baseURL  string
		expected []AnalyzedURL
	}{
		{
			name:    "dynamic import",
			js:      `import('./module.js')`,
			baseURL: "https://example.com/app.js",
			expected: []AnalyzedURL{
				{URL: "https://example.com/module.js", Type: "dynamic-import"},
			},
		},
		{
			name:    "require",
			js:      `require('./lib.js')`,
			baseURL: "https://example.com/app.js",
			expected: []AnalyzedURL{
				{URL: "https://example.com/lib.js", Type: "require"},
			},
		},
		{
			name:    "fetch",
			js:      `fetch('/api/data')`,
			baseURL: "https://example.com/app.js",
			expected: []AnalyzedURL{
				{URL: "https://example.com/api/data", Type: "fetch"},
			},
		},
		{
			name:    "axios get",
			js:      `axios.get('/api/users')`,
			baseURL: "https://example.com/app.js",
			expected: []AnalyzedURL{
				{URL: "https://example.com/api/users", Type: "axios"},
			},
		},
		{
			name:    "react lazy",
			js:      `React.lazy(() => import('./Component'))`,
			baseURL: "https://example.com/app.js",
			expected: []AnalyzedURL{
				{URL: "https://example.com/Component", Type: "dynamic-import"},
				{URL: "https://example.com/Component", Type: "react-lazy"},
			},
		},
		{
			name:    "webpack require",
			js:      `__webpack_require__("./chunk.js")`,
			baseURL: "https://example.com/app.js",
			expected: []AnalyzedURL{
				{URL: "https://example.com/chunk.js", Type: "webpack-require"},
			},
		},
		{
			name:    "absolute URL",
			js:      `fetch('https://api.example.com/data')`,
			baseURL: "https://example.com/app.js",
			expected: []AnalyzedURL{
				{URL: "https://api.example.com/data", Type: "fetch"},
			},
		},
		{
			name:    "multiple patterns",
			js:      `import('./a.js'); require('./b.js'); fetch('/api/c')`,
			baseURL: "https://example.com/app.js",
			expected: []AnalyzedURL{
				{URL: "https://example.com/a.js", Type: "dynamic-import"},
				{URL: "https://example.com/b.js", Type: "require"},
				{URL: "https://example.com/api/c", Type: "fetch"},
			},
		},
		{
			name:     "no matches",
			js:       `const x = 1; console.log(x);`,
			baseURL:  "https://example.com/app.js",
			expected: []AnalyzedURL{},
		},
		{
			name:     "data URI excluded",
			js:       `fetch('data:application/json,{}')`,
			baseURL:  "https://example.com/app.js",
			expected: []AnalyzedURL{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractJSURLs(tt.js, tt.baseURL)
			sort.Slice(result, func(i, j int) bool {
				if result[i].Type != result[j].Type {
					return result[i].Type < result[j].Type
				}
				return result[i].URL < result[j].URL
			})
			sort.Slice(tt.expected, func(i, j int) bool {
				if tt.expected[i].Type != tt.expected[j].Type {
					return tt.expected[i].Type < tt.expected[j].Type
				}
				return tt.expected[i].URL < tt.expected[j].URL
			})
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d URLs, got %d: %v", len(tt.expected), len(result), result)
			}
			for i, exp := range tt.expected {
				if i < len(result) {
					if result[i].URL != exp.URL {
						t.Errorf("URL[%d]: expected %q, got %q", i, exp.URL, result[i].URL)
					}
					if result[i].Type != exp.Type {
						t.Errorf("Type[%d]: expected %q, got %q", i, exp.Type, result[i].Type)
					}
				}
			}
		})
	}
}

func TestExtractFromHTML(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		baseURL  string
		expected []AnalyzedURL
	}{
		{
			name:    "importmap",
			html:    `<script type="importmap">{"imports": {"react": "https://cdn.example.com/react.js"}}</script>`,
			baseURL: "https://example.com/index.html",
			expected: []AnalyzedURL{
				{URL: "https://cdn.example.com/react.js", Type: "importmap"},
			},
		},
		{
			name:    "module script",
			html:    `<script type="module" src="/app.js"></script>`,
			baseURL: "https://example.com/index.html",
			expected: []AnalyzedURL{
				{URL: "https://example.com/app.js", Type: "module-script"},
			},
		},
		{
			name: "multiple",
			html: `<script type="importmap">{"imports": {"vue": "/vue.js"}}</script>
<script type="module" src="https://cdn.example.com/app.js"></script>`,
			baseURL: "https://example.com/index.html",
			expected: []AnalyzedURL{
				{URL: "https://example.com/vue.js", Type: "importmap"},
				{URL: "https://cdn.example.com/app.js", Type: "module-script"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractFromHTML(tt.html, tt.baseURL)
			sort.Slice(result, func(i, j int) bool {
				if result[i].Type != result[j].Type {
					return result[i].Type < result[j].Type
				}
				return result[i].URL < result[j].URL
			})
			sort.Slice(tt.expected, func(i, j int) bool {
				if tt.expected[i].Type != tt.expected[j].Type {
					return tt.expected[i].Type < tt.expected[j].Type
				}
				return tt.expected[i].URL < tt.expected[j].URL
			})
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d URLs, got %d: %v", len(tt.expected), len(result), result)
			}
			for i, exp := range tt.expected {
				if i < len(result) {
					if result[i].URL != exp.URL {
						t.Errorf("URL[%d]: expected %q, got %q", i, exp.URL, result[i].URL)
					}
					if result[i].Type != exp.Type {
						t.Errorf("Type[%d]: expected %q, got %q", i, exp.Type, result[i].Type)
					}
				}
			}
		})
	}
}
