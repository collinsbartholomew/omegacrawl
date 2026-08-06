package queue

import "strings"

var trackingParams = map[string]bool{
	"utm_source":      true,
	"utm_medium":      true,
	"utm_campaign":    true,
	"utm_term":        true,
	"utm_content":     true,
	"utm_id":          true,
	"utm_cid":         true,
	"utm_reader":      true,
	"utm_name":        true,
	"utm_social":      true,
	"utm_social-type": true,
	"fbclid":          true,
	"gclid":           true,
	"gclsrc":          true,
	"dclid":           true,
	"gbraid":          true,
	"wbraid":          true,
	"msclkid":         true,
	"twclid":          true,
	"li_fat_id":       true,
	"mc_cid":          true,
	"mc_eid":          true,
	"_ga":             true,
	"_gl":             true,
	"oly_anon_id":     true,
	"oly_enc_id":      true,
	"ref":             true,
	"source":          true,
	"campaign_id":     true,
	"campaign":        true,
	"medium":          true,
	"content":         true,
	"keyword":         true,
	"affiliate_id":    true,
	"click_id":        true,
	"fb_action_ids":   true,
	"fb_action_types": true,
	"fb_ref":          true,
	"fb_source":       true,
	"hsa_cam":         true,
	"hsa_grp":         true,
	"hsa_mt":          true,
	"hsa_src":         true,
	"hsa_ad":          true,
	"hsa_acc":         true,
	"hsa_net":         true,
	"hsa_ver":         true,
	"hsa_la":          true,
	"hsa_ol":          true,
	"hsa_kw":          true,
	"hsa_tgt":         true,
	"hsa_bal":         true,
	"hsa_bgt":         true,
	"hsa_wn":          true,
	"hsa_cr":          true,
	"hsa_og":          true,
	"hsa_mi":          true,
	"_kx":             true,
	"vero_id":         true,
	"wickedid":        true,
	"yclid":           true,
	"__s":             true,
	"_hsmi":           true,
	"_hsenc":          true,
	"si":              true,
	"ss":              true,
	"spm":             true,
	"scm":             true,
	"pk_campaign":     true,
	"pk_kwd":          true,
	"pk_source":       true,
	"pk_medium":       true,
	"pk_content":      true,
	"_openstat":       true,
	"vov":             true,
	"vzmid":           true,
	"from":            true,
	"spm_":            true,
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
