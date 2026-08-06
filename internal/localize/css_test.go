package localize

import "testing"

func TestRewriteCSSImportsQuotes(t *testing.T) {
	rw := NewRewriter(map[string]string{
		"http://example.com/css/theme.css": "/local/css/theme.css",
	}, nil, "/local")

	in := `@import "theme.css"; @import 'theme.css' layer(foo);`
	want := `@import "theme.css"; @import 'theme.css' layer(foo);`
	got := rw.rewriteCSSImports(in, "http://example.com/css/style.css", "/local/css")
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestRewriteCSSImportsLeavesUnresolved(t *testing.T) {
	rw := NewRewriter(nil, nil, "/local")

	in := `@import "missing.css"; @import url(foo.css);`
	got := rw.rewriteCSSImports(in, "http://example.com/css/style.css", "/local/css")
	if got != in {
		t.Errorf("expected unchanged %q, got %q", in, got)
	}
}

func TestCSSImportPatternDoesNotPanic(t *testing.T) {
	cases := []string{
		`@import "a.css";`,
		`@import 'b.css' screen;`,
		`@import "c.css" layer(base);`,
		`@import url(d.css);`,
		``,
	}
	for _, c := range cases {
		// Compile-time assertion: the pattern must be a valid Go (RE2) regexp.
		if cssImportQuotedPattern == nil {
			t.Fatal("cssImportQuotedPattern must compile")
		}
		cssImportQuotedPattern.FindString(c)
	}
}
