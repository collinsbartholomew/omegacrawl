package queue

import (
	"net/url"
	"sort"
	"strings"
)

var trackingParams = map[string]bool{
	"utm_source":    true,
	"utm_medium":    true,
	"utm_campaign":  true,
	"utm_term":      true,
	"utm_content":   true,
	"utm_id":        true,
	"utm_cid":       true,
	"utm_reader":    true,
	"utm_name":      true,
	"utm_social":    true,
	"utm_social-type": true,
	"fbclid":        true,
	"gclid":         true,
	"gclsrc":        true,
	"dclid":         true,
	"gbraid":        true,
	"wbraid":        true,
	"msclkid":       true,
	"twclid":        true,
	"li_fat_id":     true,
	"mc_cid":        true,
	"mc_eid":        true,
	"_ga":           true,
	"_gl":           true,
	"oly_anon_id":   true,
	"oly_enc_id":    true,
	"ref":           true,
	"source":        true,
	"campaign_id":   true,
	"campaign":      true,
	"medium":        true,
	"content":       true,
	"keyword":       true,
	"affiliate_id":  true,
	"click_id":      true,
	"fb_action_ids": true,
	"fb_action_types": true,
	"fb_ref":        true,
	"fb_source":     true,
	"hsa_cam":       true,
	"hsa_grp":       true,
	"hsa_mt":        true,
	"hsa_src":       true,
	"hsa_ad":        true,
	"hsa_acc":       true,
	"hsa_net":       true,
	"hsa_ver":       true,
	"hsa_la":        true,
	"hsa_ol":        true,
	"hsa_kw":        true,
	"hsa_tgt":       true,
	"hsa_bal":       true,
	"hsa_bgt":       true,
	"hsa_wn":        true,
	"hsa_cr":        true,
	"hsa_og":        true,
	"hsa_mi":        true,
	"_kx":           true,
	"vero_id":       true,
	"wickedid":      true,
	"yclid":         true,
	"__s":           true,
	"_hsmi":         true,
	"_hsenc":        true,
	"si":            true,
	"ss":            true,
	"spm":           true,
	"scm":           true,
	"pk_campaign":   true,
	"pk_kwd":        true,
	"pk_source":     true,
	"pk_medium":     true,
	"pk_content":    true,
	"_openstat":     true,
	"vov":           true,
	"vzmid":         true,
	"from":          true,
	"spm_":          true,
}

var defaultPorts = map[string]string{
	"http":  "",
	"https": "",
}

func NormalizeURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return rawURL
	}

	// Keep original scheme, don't force HTTPS

	host := strings.ToLower(u.Hostname())
	if host == "" {
		return rawURL
	}

	port := u.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}

	var hostStr string
	if port != "" {
		hostStr = host + ":" + port
	} else {
		hostStr = host
	}

	path := u.Path
	if path == "" {
		path = "/"
	}

	path = normalizePath(path)

	cleanURL := scheme + "://" + hostStr + path

	if u.RawQuery != "" {
		sortedQuery := sortQueryParams(u.RawQuery)
		if sortedQuery != "" {
			cleanURL += "?" + sortedQuery
		}
	}

	return cleanURL
}

func normalizePath(path string) string {
	if path == "" {
		return "/"
	}

	path = strings.ReplaceAll(path, "/./", "/")

	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}

	segments := strings.Split(path, "/")
	var resolved []string
	for _, seg := range segments {
		if seg == ".." {
			if len(resolved) > 0 {
				resolved = resolved[:len(resolved)-1]
			}
		} else if seg != "." {
			resolved = append(resolved, seg)
		}
	}

	path = strings.Join(resolved, "/")

	if len(path) > 1 && strings.HasSuffix(path, "/") {
		path = strings.TrimSuffix(path, "/")
	}

	if path == "" {
		path = "/"
	}

	path = strings.ReplaceAll(path, "%7E", "~")

	return path
}

func sortQueryParams(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}

	params, err := url.ParseQuery(rawQuery)
	if err != nil {
		return rawQuery
	}

	var keys []string
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var result []string
	for _, k := range keys {
		if shouldStripParam(k) {
			continue
		}
		vals := params[k]
		for _, v := range vals {
			if v == "" {
				result = append(result, k)
			} else {
				result = append(result, k+"="+v)
			}
		}
	}

	return strings.Join(result, "&")
}

func shouldStripParam(key string) bool {
	lower := strings.ToLower(key)

	if trackingParams[lower] {
		return true
	}

	if strings.HasPrefix(lower, "utm_") {
		return true
	}

	if strings.HasPrefix(lower, "_hs") {
		return true
	}

	return false
}

func NormalizeAndClean(rawURL string) string {
	normalized := NormalizeURL(rawURL)

	u, err := url.Parse(normalized)
	if err != nil {
		return normalized
	}

	// Preserve hash fragments for SPA routing
	if u.Fragment != "" && !strings.HasPrefix(u.Fragment, "~") {
		return u.String()
	}

	u.Fragment = ""
	return u.String()
}
