// Package wechat pushes article drafts into a WeChat Official Account's draft
// box and, when the owner asks for it, submits them for publishing. It only
// talks to the fixed official host api.weixin.qq.com.
package wechat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const apiBase = "https://api.weixin.qq.com/cgi-bin"

var ErrNotConfigured = errors.New("AppID and AppSecret must be configured on the settings screen")

// APIError is an errcode/errmsg answer from the WeChat API, or a transport
// failure. It never carries the access token or the URL it was sent on.
type APIError struct {
	Op      string
	Code    int
	Message string
}

func (e *APIError) Error() string {
	if e.Code != 0 {
		return fmt.Sprintf("WeChat %s failed: errcode=%d errmsg=%s", e.Op, e.Code, e.Message)
	}
	return fmt.Sprintf("WeChat %s failed: %s", e.Op, e.Message)
}

type Client struct {
	appID, secret string
	httpClient    *http.Client

	mu      sync.Mutex
	token   string
	expires time.Time
}

// New builds a client for one account. proxyURL is a socks5:// URL or empty for
// a direct connection; an invalid URL is rejected here so the settings screen
// can report it instead of failing at push time.
func New(appID, secret, proxyURL string) (*Client, error) {
	proxy, err := parseProxy(proxyURL)
	if err != nil {
		return nil, err
	}
	client := &Client{
		appID: strings.TrimSpace(appID), secret: strings.TrimSpace(secret),
		httpClient: &http.Client{
			Timeout:       60 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
	if proxy != nil {
		client.httpClient = proxy.client()
	}
	return client, nil
}

func (c *Client) Configured() bool { return c.appID != "" && c.secret != "" }

// Test fetches an access token, which is the cheapest call that proves the
// credentials work. The token itself is discarded.
func (c *Client) Test(ctx context.Context) error {
	_, err := c.accessToken(ctx)
	return err
}

// Article is one draft-box article. Content is HTML.
type Article struct {
	Title        string `json:"title"`
	Author       string `json:"author,omitempty"`
	Digest       string `json:"digest"`
	Content      string `json:"content"`
	ThumbMediaID string `json:"thumb_media_id"`
	OpenComment  int    `json:"need_open_comment"`
	FansComment  int    `json:"only_fans_can_comment"`
}

// GalleryDraft is one picture post (article_type=newspic): a short plain-text
// caption and up to twenty permanent image media_ids. The first image is the
// cover.
type GalleryDraft struct {
	ArticleType string `json:"article_type"`
	Title       string `json:"title"`
	Content     string `json:"content"`
	ImageInfo   struct {
		ImageList []struct {
			MediaID string `json:"image_media_id"`
		} `json:"image_list"`
	} `json:"image_info"`
	OpenComment int `json:"need_open_comment"`
	FansComment int `json:"only_fans_can_comment"`
}

// UploadImage stores an image as permanent material and returns its media_id.
// It is what both a long article's cover and every picture-post image need.
func (c *Client) UploadImage(ctx context.Context, filename string, data []byte) (string, error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return "", err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("media", filename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	endpoint := apiBase + "/material/add_material?type=image&access_token=" + url.QueryEscape(token)
	var out struct {
		MediaID string `json:"media_id"`
	}
	if err := c.do(ctx, "upload image", endpoint, writer.FormDataContentType(), &body, &out); err != nil {
		c.dropTokenOn(err)
		return "", err
	}
	if out.MediaID == "" {
		return "", &APIError{Op: "upload image", Message: "response has no media_id"}
	}
	return out.MediaID, nil
}

// UploadContentImage stores an image used inside a news article and returns the
// URL to put in the content. WeChat only accepts image URLs it produced itself.
func (c *Client) UploadContentImage(ctx context.Context, filename string, data []byte) (string, error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return "", err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("media", filename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	endpoint := apiBase + "/media/uploadimg?access_token=" + url.QueryEscape(token)
	var out struct {
		URL string `json:"url"`
	}
	if err := c.do(ctx, "upload content image", endpoint, writer.FormDataContentType(), &body, &out); err != nil {
		c.dropTokenOn(err)
		return "", err
	}
	if out.URL == "" {
		return "", &APIError{Op: "upload content image", Message: "response has no url"}
	}
	return out.URL, nil
}

// AddGalleryDraft creates one picture post in the draft box.
func (c *Client) AddGalleryDraft(ctx context.Context, title, content string, imageIDs []string) (string, error) {
	if len(imageIDs) == 0 {
		return "", &APIError{Op: "add gallery draft", Message: "a picture post needs at least one image"}
	}
	token, err := c.accessToken(ctx)
	if err != nil {
		return "", err
	}
	draft := GalleryDraft{ArticleType: "newspic", Title: title, Content: content}
	for _, id := range imageIDs {
		draft.ImageInfo.ImageList = append(draft.ImageInfo.ImageList, struct {
			MediaID string `json:"image_media_id"`
		}{MediaID: id})
	}
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(map[string]any{"articles": []GalleryDraft{draft}}); err != nil {
		return "", err
	}
	endpoint := apiBase + "/draft/add?access_token=" + url.QueryEscape(token)
	var out struct {
		MediaID string `json:"media_id"`
	}
	if err := c.do(ctx, "add gallery draft", endpoint, "application/json", &body, &out); err != nil {
		c.dropTokenOn(err)
		return "", err
	}
	if out.MediaID == "" {
		return "", &APIError{Op: "add gallery draft", Message: "response has no media_id"}
	}
	return out.MediaID, nil
}

// AddDraft creates one draft-box entry and returns its media_id.
func (c *Client) AddDraft(ctx context.Context, article Article) (string, error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return "", err
	}
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	// Keep HTML and CJK literal; the API stores whatever escapes it receives.
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(map[string]any{"articles": []Article{article}}); err != nil {
		return "", err
	}
	endpoint := apiBase + "/draft/add?access_token=" + url.QueryEscape(token)
	var out struct {
		MediaID string `json:"media_id"`
	}
	if err := c.do(ctx, "add draft", endpoint, "application/json", &body, &out); err != nil {
		c.dropTokenOn(err)
		return "", err
	}
	if out.MediaID == "" {
		return "", &APIError{Op: "add draft", Message: "response has no media_id"}
	}
	return out.MediaID, nil
}

// SubmitPublish asks WeChat to publish a draft-box entry (freepublish/submit)
// and returns the publish_id. Publishing is asynchronous on WeChat's side, and
// many accounts (unverified or personal ones) have no permission for it, in
// which case the API's own errcode is returned unchanged.
func (c *Client) SubmitPublish(ctx context.Context, mediaID string) (string, error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(map[string]string{"media_id": mediaID})
	if err != nil {
		return "", err
	}
	endpoint := apiBase + "/freepublish/submit?access_token=" + url.QueryEscape(token)
	var out struct {
		PublishID json.RawMessage `json:"publish_id"`
	}
	if err := c.do(ctx, "submit publish", endpoint, "application/json", bytes.NewReader(payload), &out); err != nil {
		c.dropTokenOn(err)
		return "", err
	}
	publishID := strings.Trim(string(out.PublishID), `"`)
	if publishID == "" || publishID == "null" {
		return "", &APIError{Op: "submit publish", Message: "response has no publish_id"}
	}
	return publishID, nil
}

// accessToken uses the stable_token endpoint, which does not invalidate a
// token another tool of the same account is already using.
func (c *Client) accessToken(ctx context.Context) (string, error) {
	if !c.Configured() {
		return "", ErrNotConfigured
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.expires) {
		return c.token, nil
	}
	payload, _ := json.Marshal(map[string]string{
		"grant_type": "client_credential", "appid": c.appID, "secret": c.secret,
	})
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := c.do(ctx, "get access token", apiBase+"/stable_token", "application/json", bytes.NewReader(payload), &out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", &APIError{Op: "get access token", Message: "response has no access_token"}
	}
	c.token = out.AccessToken
	c.expires = time.Now().Add(time.Duration(max(out.ExpiresIn-300, 60)) * time.Second)
	return c.token, nil
}

// dropTokenOn forgets a cached token the API reports as invalid or expired,
// so the next call fetches a fresh one.
func (c *Client) dropTokenOn(err error) {
	var apiErr *APIError
	if errors.As(err, &apiErr) && (apiErr.Code == 40001 || apiErr.Code == 42001 || apiErr.Code == 40014) {
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
	}
}

func (c *Client) do(ctx context.Context, op, endpoint, contentType string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return &APIError{Op: op, Message: "build request failed"}
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// The transport error embeds the URL, which carries the access
		// token, so only its kind is reported.
		message := "network request failed"
		if errors.Is(err, context.DeadlineExceeded) {
			message = "request timed out"
		}
		return &APIError{Op: op, Message: message}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return &APIError{Op: op, Message: "read response failed"}
	}
	var status struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(data, &status); err != nil {
		return &APIError{Op: op, Message: fmt.Sprintf("HTTP %d, response is not JSON", resp.StatusCode)}
	}
	if status.ErrCode != 0 {
		return &APIError{Op: op, Code: status.ErrCode, Message: status.ErrMsg}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{Op: op, Message: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	return json.Unmarshal(data, out)
}
