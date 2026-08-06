package rewrite

import "golang.org/x/net/html/atom"

var urlAttrs = map[atom.Atom]bool{
	atom.Href:   true,
	atom.Src:    true,
	atom.Action: true,
	atom.Poster: true,
}

var dataURLAttrs = map[string]bool{
	"data-src":         true,
	"data-srcset":      true,
	"data-lazy-src":    true,
	"data-original":    true,
	"data-image":       true,
	"data-bg":          true,
	"data-background":  true,
	"data-href":        true,
	"data-url":         true,
	"data-srcpath":     true,
	"data-request":     true,
	"data-endpoint":    true,
	"data-lazy":        true,
	"data-delayed-url": true,
	"data-settings":    true,
}

func isURLAttr(key []byte) bool {
	a := atom.Lookup(key)
	if a != 0 && urlAttrs[a] {
		return true
	}
	return dataURLAttrs[string(key)]
}
