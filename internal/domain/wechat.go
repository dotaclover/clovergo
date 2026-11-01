package domain

import "net/url"

// WechatSettings is the owner's Official Account configuration: the credentials
// used to push a draft into its draft box, plus the defaults the push form
// starts from.
//
// AppSecret is never written to logs or exported; it is stored only in the
// local SQLite so the settings screen can change it without editing .env.
type WechatSettings struct {
	AppID        string `json:"app_id"`
	AppSecret    string `json:"app_secret"`
	Author       string `json:"author"`
	ThumbMediaID string `json:"thumb_media_id"`
	// ProxyURL is an optional socks5:// URL. The push goes through it so a
	// local install can keep one stable address in the account allowlist.
	ProxyURL  string `json:"proxy_url"`
	UpdatedAt string `json:"updated_at"`
	// AppSecretSet tells the settings screen a secret is stored; the secret
	// itself is never returned (app_secret is always "" in responses).
	AppSecretSet bool `json:"app_secret_set"`
}

// Public is the copy returned to the settings screen: the secret is replaced
// by AppSecretSet.
func (w WechatSettings) Public() WechatSettings {
	w.AppSecretSet = w.AppSecret != ""
	w.AppSecret = ""
	if w.ProxyURL != "" {
		proxy, err := url.Parse(w.ProxyURL)
		if err != nil {
			w.ProxyURL = "[invalid proxy URL]"
		} else {
			if proxy.User != nil {
				proxy.User = url.UserPassword("redacted", "redacted")
			}
			w.ProxyURL = proxy.String()
		}
	}
	return w
}

// Configured reports whether a push can be attempted at all.
func (w WechatSettings) Configured() bool {
	return w.AppID != "" && w.AppSecret != ""
}
