package captcha

const (
	// Provider2Captcha uses the 2captcha service.
	Provider2Captcha Provider = "2captcha"
	// ProviderAntiCaptcha uses the Anti-Captcha service.
	ProviderAntiCaptcha Provider = "anticaptcha"
	// ProviderCapMonster uses the CapMonster service.
	ProviderCapMonster Provider = "capmonster"
)

const (
	// TypeRecaptchaV2 is a reCAPTCHA v2 challenge.
	TypeRecaptchaV2 CAPTCHAType = "recaptcha_v2"
	// TypeRecaptchaV3 is a reCAPTCHA v3 challenge.
	TypeRecaptchaV3 CAPTCHAType = "recaptcha_v3"
	// TypeHCaptcha is an hCaptcha challenge.
	TypeHCaptcha CAPTCHAType = "hcaptcha"
	// TypeImageCaptcha is an image-based CAPTCHA.
	TypeImageCaptcha CAPTCHAType = "image_captcha"
	// TypeTurnstile is a Cloudflare Turnstile challenge.
	TypeTurnstile CAPTCHAType = "turnstile"
)
