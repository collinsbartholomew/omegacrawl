package queue

import (
	"net/url"
	"strings"
)

// GlobMatch reports whether the pattern matches the string using glob semantics
// with forward slashes as path separators (URL-style), regardless of OS.
// Pattern syntax:
//   - matches any sequence of non-separator characters
//     ** matches any sequence including separators
//     ? matches any single non-separator character
//     [abc] matches any character in the set
//     [a-z] matches any character in the range
func GlobMatch(pattern, s string) bool {
	return globMatchInternal(pattern, s, true)
}

// globMatchInternal performs the actual glob matching.
// If pathStyle is true, / is treated as a path separator for ** matching.
func globMatchInternal(pattern, s string, pathStyle bool) bool {
	// Convert glob pattern to a simple state machine
	// This is a simplified implementation for common cases
	// For full glob support, we'd need a more complex implementation

	// Handle ** (match any including separators) specially
	if strings.Contains(pattern, "**") {
		return globMatchWithDoubleStar(pattern, s, pathStyle)
	}

	// Simple case: no **, just * and ?
	return globMatchSimple(pattern, s, pathStyle)
}

// globMatchWithDoubleStar handles patterns containing **
func globMatchWithDoubleStar(pattern, s string, pathStyle bool) bool {
	// Split pattern by **
	parts := strings.Split(pattern, "**")
	if len(parts) == 1 {
		return globMatchSimple(pattern, s, pathStyle)
	}

	// Match prefix
	idx := 0
	if !strings.HasPrefix(pattern, "**") {
		if !globMatchSimple(parts[0], s, pathStyle) {
			return false
		}
		idx = len(parts[0])
		parts = parts[1:]
	}

	// Match middle parts
	for _, part := range parts[:len(parts)-1] {
		if part == "" {
			continue
		}
		found := false
		for j := idx; j <= len(s); j++ {
			if globMatchSimple(part, s[idx:j], pathStyle) {
				idx = j
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Match suffix
	if !strings.HasSuffix(pattern, "**") {
		return globMatchSimple(parts[len(parts)-1], s[idx:], pathStyle)
	}
	return true
}

// globMatchSimple handles patterns with only * and ? (no **)
func globMatchSimple(pattern, s string, pathStyle bool) bool {
	p := 0
	for i := 0; i < len(s); i++ {
		if p >= len(pattern) {
			return false
		}
		switch pattern[p] {
		case '*':
			// Match any sequence of non-separator chars
			p++
			if p >= len(pattern) {
				// * at end matches rest
				if pathStyle {
					// Ensure no separator in rest
					return !strings.Contains(s[i:], "/")
				}
				return true
			}
			// Match until next pattern char
			nextPat := pattern[p]
			for ; i < len(s); i++ {
				if pathStyle && s[i] == '/' {
					break
				}
				if s[i] == nextPat || (nextPat == '?' && s[i] != '/') || (nextPat == '[' && matchBracket(pattern, &p, s[i])) {
					break
				}
			}
			if i == len(s) {
				return false
			}
		case '?':
			// Match any single non-separator char
			if pathStyle && s[i] == '/' {
				return false
			}
			p++
		case '[':
			// Character class
			if !matchBracket(pattern, &p, s[i]) {
				return false
			}
			p++ // skip past ]
		default:
			if pattern[p] != s[i] {
				return false
			}
			p++
		}
	}

	// Check if remaining pattern is all * (which can match empty)
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}

// matchBracket matches a character class like [abc] or [a-z]
func matchBracket(pattern string, p *int, c byte) bool {
	*p++ // skip [
	if *p >= len(pattern) {
		return false
	}
	negate := false
	if pattern[*p] == '!' || pattern[*p] == '^' {
		negate = true
		*p++
	}

	for *p < len(pattern) && pattern[*p] != ']' {
		if *p+2 < len(pattern) && pattern[*p+1] == '-' && pattern[*p+2] != ']' {
			// Range like a-z
			start := pattern[*p]
			end := pattern[*p+2]
			if c >= start && c <= end {
				*p += 3
				for *p < len(pattern) && pattern[*p] != ']' {
					*p++
				}
				return !negate
			}
			*p += 3
		} else {
			// Single character
			if pattern[*p] == c {
				*p++
				for *p < len(pattern) && pattern[*p] != ']' {
					*p++
				}
				return !negate
			}
			*p++
		}
	}
	return negate
}

// PathGlobMatch matches a URL path against a glob pattern.
// The pattern is matched against the URL path only (not the full URL).
func PathGlobMatch(pattern, urlPath string) bool {
	// Normalize both to use forward slashes
	urlPath = strings.ReplaceAll(urlPath, "\\", "/")
	pattern = strings.ReplaceAll(pattern, "\\", "/")
	return GlobMatch(pattern, urlPath)
}

// URLGlobMatch matches a full URL against a glob pattern.
// The pattern can match against the full URL or just the path (if it starts with /).
func URLGlobMatch(pattern, rawURL string) bool {
	if strings.HasPrefix(pattern, "/") {
		// Path-only pattern
		if u, err := url.Parse(rawURL); err == nil {
			return PathGlobMatch(pattern, u.Path)
		}
		return false
	}
	// Full URL pattern
	return GlobMatch(pattern, rawURL)
}
