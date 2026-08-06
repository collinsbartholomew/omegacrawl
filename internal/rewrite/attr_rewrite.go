package rewrite

import (
	"bytes"
	"path/filepath"
	"strings"

	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

func rewriteScriptStyleURLs(text []byte, baseURL string, mappings map[string]string, pathCache map[string]string, absRelCache map[string]string) []byte {
	result := text

	var pairs [][2][]byte
	for absURL, relPath := range absRelCache {
		pairs = append(pairs, [2][]byte{[]byte(absURL), []byte(relPath)})
	}
	for absURL, relPath := range pathCache {
		pairs = append(pairs, [2][]byte{[]byte(absURL), []byte(relPath)})
	}
	if len(pairs) > 0 {
		result = batchReplace(result, pairs)
	}

	result = scriptURLPattern.ReplaceAllFunc(result, func(match []byte) []byte {
		sub := scriptURLPattern.FindSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		var quote []byte
		var urlStr string
		if len(sub[1]) > 0 {
			quote = sub[1][:1]
			urlStr = string(sub[1])
		} else if len(sub[2]) > 0 {
			quote = sub[2][:1]
			urlStr = string(sub[2])
		} else {
			return match
		}

		if strings.HasPrefix(urlStr, "http://") || strings.HasPrefix(urlStr, "https://") {
			if relPath, ok := pathCache[urlStr]; ok {
				return []byte(string(quote) + relPath + string(quote))
			}
			if relPath, ok := absRelCache[urlStr]; ok {
				return []byte(string(quote) + relPath + string(quote))
			}
		} else if baseURL != "" {
			resolved := ResolveURL(baseURL, urlStr)
			if relPath, ok := pathCache[resolved]; ok {
				return []byte(string(quote) + relPath + string(quote))
			}
			if relPath, ok := absRelCache[resolved]; ok {
				return []byte(string(quote) + relPath + string(quote))
			}
		}
		return match
	})

	result = cssURLPattern.ReplaceAllFunc(result, func(match []byte) []byte {
		sub := cssURLPattern.FindSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		urlStr := string(bytes.TrimSpace(sub[1]))
		urlStr = strings.Trim(urlStr, `"' `)

		if relPath, ok := pathCache[urlStr]; ok {
			return bytes.Replace(match, sub[1], []byte(relPath), 1)
		}
		if relPath, ok := absRelCache[urlStr]; ok {
			return bytes.Replace(match, sub[1], []byte(relPath), 1)
		}

		if baseURL != "" && !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") && !strings.HasPrefix(urlStr, "data:") {
			resolved := ResolveURL(baseURL, urlStr)
			if resolved != "" && resolved != urlStr {
				if relPath, ok := pathCache[resolved]; ok {
					return bytes.Replace(match, sub[1], []byte(relPath), 1)
				}
				if relPath, ok := absRelCache[resolved]; ok {
					return bytes.Replace(match, sub[1], []byte(relPath), 1)
				}
			}
		}
		return match
	})

	return result
}

func rewriteAttrVal(val string, htmlDir string, baseURL string, mappings map[string]string, pathCache map[string]string, absRelCache map[string]string) (string, bool) {
	if val == "" || strings.HasPrefix(val, "#") || strings.HasPrefix(val, "javascript:") || strings.HasPrefix(val, "mailto:") || strings.HasPrefix(val, "tel:") || strings.HasPrefix(val, "data:") {
		return "", false
	}

	if strings.Contains(val, ",") && (containsURL(mappings, val) || containsURL(absRelCache, val)) {
		return rewriteSrcset(val, baseURL, mappings, pathCache, absRelCache)
	}

	if strings.HasPrefix(val, "http://") || strings.HasPrefix(val, "https://") {
		if relPath, ok := pathCache[val]; ok {
			return relPath, true
		}
		if relPath, ok := absRelCache[val]; ok {
			return relPath, true
		}
	}

	if baseURL != "" {
		resolved := ResolveURL(baseURL, val)
		if resolved != "" && resolved != val {
			if relPath, ok := pathCache[resolved]; ok {
				return relPath, true
			}
			if relPath, ok := absRelCache[resolved]; ok {
				return relPath, true
			}
		}
	}

	return "", false
}

func rewriteSrcset(val string, baseURL string, mappings map[string]string, pathCache map[string]string, absRelCache map[string]string) (string, bool) {
	parts := strings.Split(val, ",")
	changed := false
	for i, part := range parts {
		part = strings.TrimSpace(part)
		urlPart := strings.Split(part, " ")[0]
		if urlPart == "" {
			continue
		}

		if relPath, ok := pathCache[urlPart]; ok {
			parts[i] = relPath + strings.TrimPrefix(part, urlPart)
			changed = true
			continue
		}
		if relPath, ok := absRelCache[urlPart]; ok {
			parts[i] = relPath + strings.TrimPrefix(part, urlPart)
			changed = true
			continue
		}

		if baseURL != "" && !strings.HasPrefix(urlPart, "http://") && !strings.HasPrefix(urlPart, "https://") && !strings.HasPrefix(urlPart, "data:") {
			resolved := ResolveURL(baseURL, urlPart)
			if resolved != "" && resolved != urlPart {
				if relPath, ok := pathCache[resolved]; ok {
					parts[i] = relPath + strings.TrimPrefix(part, urlPart)
					changed = true
					continue
				}
				if relPath, ok := absRelCache[resolved]; ok {
					parts[i] = relPath + strings.TrimPrefix(part, urlPart)
					changed = true
					continue
				}
			}
		}
	}
	if changed {
		return strings.Join(parts, ", "), true
	}
	return "", false
}

func containsURL(mappings map[string]string, val string) bool {
	for url := range mappings {
		if strings.Contains(val, url) {
			return true
		}
	}
	return false
}

func rewriteInlineCSSURLs(styleVal []byte, htmlDir string, baseURL string, mappings map[string]string, pathCache map[string]string, absRelCache map[string]string) []byte {
	result := cssURLPattern.ReplaceAllFunc(styleVal, func(match []byte) []byte {
		sub := cssURLPattern.FindSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		urlStr := string(bytes.TrimSpace(sub[1]))
		urlStr = strings.Trim(urlStr, `"' `)

		if localPath, ok := mappings[urlStr]; ok {
			relPath, err := filepath.Rel(htmlDir, localPath)
			if err != nil {
				relPath = localPath
			}
			relPath = filepath.ToSlash(relPath)
			return bytes.Replace(match, sub[1], []byte(relPath), 1)
		}

		if relPath, ok := pathCache[urlStr]; ok {
			return bytes.Replace(match, sub[1], []byte(relPath), 1)
		}
		if relPath, ok := absRelCache[urlStr]; ok {
			return bytes.Replace(match, sub[1], []byte(relPath), 1)
		}

		if baseURL != "" && !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") && !strings.HasPrefix(urlStr, "data:") {
			resolved := ResolveURL(baseURL, urlStr)
			if resolved != "" && resolved != urlStr {
				if relPath, ok := pathCache[resolved]; ok {
					return bytes.Replace(match, sub[1], []byte(relPath), 1)
				}
				if relPath, ok := absRelCache[resolved]; ok {
					return bytes.Replace(match, sub[1], []byte(relPath), 1)
				}
			}
		}
		return match
	})

	result = cssImportPattern.ReplaceAllFunc(result, func(match []byte) []byte {
		matches := cssImportPattern.FindSubmatch(match)
		if len(matches) > 1 {
			urlStr := string(matches[1])
			urlStr = strings.Trim(urlStr, `"' `)
			if localPath, ok := mappings[urlStr]; ok {
				relPath, err := filepath.Rel(htmlDir, localPath)
				if err != nil {
					util.LogError("failed to compute relative path for import", err, zap.String("baseDir", htmlDir), zap.String("localPath", localPath))
					relPath = localPath
				}
				relPath = filepath.ToSlash(relPath)
				return bytes.Replace(match, matches[1], []byte(relPath), 1)
			}
			if relPath, ok := pathCache[urlStr]; ok {
				return bytes.Replace(match, matches[1], []byte(relPath), 1)
			}
			if relPath, ok := absRelCache[urlStr]; ok {
				return bytes.Replace(match, matches[1], []byte(relPath), 1)
			}
			if baseURL != "" && !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") && !strings.HasPrefix(urlStr, "data:") {
				resolved := ResolveURL(baseURL, urlStr)
				if resolved != "" && resolved != urlStr {
					if relPath, ok := pathCache[resolved]; ok {
						return bytes.Replace(match, matches[1], []byte(relPath), 1)
					}
					if relPath, ok := absRelCache[resolved]; ok {
						return bytes.Replace(match, matches[1], []byte(relPath), 1)
					}
				}
			}
		}
		return match
	})

	return result
}

func replaceQuotedURLs(input []byte, sortedURLs []string, pathCache map[string]string) []byte {
	var pairs [][2][]byte
	for _, origURL := range sortedURLs {
		relPath := pathCache[origURL]
		pairs = append(pairs,
			[2][]byte{[]byte(`"` + origURL + `"`), []byte(`"` + relPath + `"`)},
			[2][]byte{[]byte(`'` + origURL + `'`), []byte(`'` + relPath + `'`)},
			[2][]byte{[]byte("`" + origURL + "`"), []byte("`" + relPath + "`")},
		)
	}
	return batchReplace(input, pairs)
}

func replaceAbsoluteURLs(input []byte, absRelCache map[string]string) []byte {
	var pairs [][2][]byte
	for absURL, relPath := range absRelCache {
		pairs = append(pairs, [2][]byte{[]byte(absURL), []byte(relPath)})
	}
	return batchReplace(input, pairs)
}
