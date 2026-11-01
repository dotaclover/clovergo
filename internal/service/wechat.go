package service

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strings"
	"unicode/utf8"

	"clovergo/internal/domain"
	"clovergo/internal/imaging"
	"clovergo/internal/platforms"
	"clovergo/internal/wechat"
)

const (
	wechatDigestMaxRunes = 120
	wechatCoverMaxBytes  = 10 << 20
)

// UseWechat seeds the Official Account settings from the environment. The seed
// is used only until the owner saves the settings screen: from then on the
// stored row wins, so the configuration can be changed without restarting.
func (s *Service) UseWechat(appID, appSecret, author, defaultThumb, proxyURL string) {
	s.wechatDefaults = domain.WechatSettings{
		AppID: strings.TrimSpace(appID), AppSecret: strings.TrimSpace(appSecret),
		Author: strings.TrimSpace(author), ThumbMediaID: strings.TrimSpace(defaultThumb),
		ProxyURL: strings.TrimSpace(proxyURL),
	}
}

// wechatSettings returns the settings in effect: the stored row when the owner
// has saved one, otherwise the environment seed.
func (s *Service) wechatSettings(ctx context.Context) (domain.WechatSettings, error) {
	settings, err := s.repo.WechatSettings(ctx)
	if err != nil {
		return domain.WechatSettings{}, appError(CodeInternal, "read wechat settings failed", err)
	}
	if settings.UpdatedAt == "" {
		return s.wechatDefaults, nil
	}
	return *settings, nil
}

// GetWechatSettings returns the configuration shown on the settings screen.
// The AppSecret is never returned; AppSecretSet says whether one is stored.
func (s *Service) GetWechatSettings(ctx context.Context) (domain.WechatSettings, error) {
	settings, err := s.wechatSettings(ctx)
	if err != nil {
		return domain.WechatSettings{}, err
	}
	return settings.Public(), nil
}

// wechatClientFor returns a client for the settings in effect, reusing the cached
// one (and its access token) while AppID, AppSecret and proxy are unchanged.
func (s *Service) wechatClientFor(settings domain.WechatSettings) (*wechat.Client, error) {
	key := [3]string{settings.AppID, settings.AppSecret, settings.ProxyURL}
	s.wechatMu.Lock()
	defer s.wechatMu.Unlock()
	if s.wechatClient != nil && s.wechatKey == key {
		return s.wechatClient, nil
	}
	client, err := wechat.New(settings.AppID, settings.AppSecret, settings.ProxyURL)
	if err != nil {
		return nil, appError(CodeInvalidInput, "the saved proxy URL is invalid", err)
	}
	s.wechatClient, s.wechatKey = client, key
	return client, nil
}

// WechatStatus reports whether a push can be attempted and whether the default
// cover is set, for the health endpoint.
func (s *Service) WechatStatus(ctx context.Context) (bool, bool) {
	settings, err := s.wechatSettings(ctx)
	if err != nil {
		return false, false
	}
	return settings.Configured(), settings.ThumbMediaID != ""
}

// SaveWechatSettings stores the owner's configuration. Credentials are either
// both present or both empty; an empty pair disables pushing. Because the
// secret is never sent back to the screen, an AppID with an empty AppSecret
// keeps the secret already in effect.
func (s *Service) SaveWechatSettings(ctx context.Context, input domain.WechatSettings) (*domain.WechatSettings, error) {
	input.AppID = strings.TrimSpace(input.AppID)
	input.AppSecret = strings.TrimSpace(input.AppSecret)
	input.Author = strings.TrimSpace(input.Author)
	input.ThumbMediaID = strings.TrimSpace(input.ThumbMediaID)
	input.ProxyURL = strings.TrimSpace(input.ProxyURL)
	current, err := s.wechatSettings(ctx)
	if err != nil {
		return nil, err
	}
	// An unchanged masked URL preserves the stored proxy credentials. Empty
	// explicitly removes the proxy; a different URL replaces it.
	if input.ProxyURL == current.Public().ProxyURL {
		input.ProxyURL = current.ProxyURL
	}
	if input.AppID != "" && input.AppSecret == "" {
		input.AppSecret = current.AppSecret
	}
	if (input.AppID == "") != (input.AppSecret == "") {
		return nil, appError(CodeInvalidInput, "AppID and AppSecret must be filled in together", nil)
	}
	if utf8.RuneCountInString(input.Author) > 32 {
		return nil, appError(CodeInvalidInput, "author must not exceed 32 characters", nil)
	}
	if utf8.RuneCountInString(input.ThumbMediaID) > 200 {
		return nil, appError(CodeInvalidInput, "the default cover media_id is too long", nil)
	}
	if err := wechat.ValidateProxy(input.ProxyURL); err != nil {
		return nil, appError(CodeInvalidInput, err.Error(), nil)
	}
	saved, err := s.repo.SaveWechatSettings(ctx, input)
	if err != nil {
		return nil, appError(CodeInternal, "save wechat settings failed", err)
	}
	s.log.InfoContext(ctx, "wechat settings saved", "configured", saved.Configured())
	public := saved.Public()
	return &public, nil
}

// TestWechat checks the stored credentials against the stable_token endpoint.
// It never logs or returns the secret.
func (s *Service) TestWechat(ctx context.Context) error {
	settings, err := s.wechatSettings(ctx)
	if err != nil {
		return err
	}
	if !settings.Configured() {
		return appError(CodeWechatNotConfigured, wechat.ErrNotConfigured.Error(), nil)
	}
	// A fresh client, so the test really fetches a token instead of reusing
	// the one cached for pushes.
	client, err := wechat.New(settings.AppID, settings.AppSecret, settings.ProxyURL)
	if err != nil {
		return appError(CodeInvalidInput, "the saved proxy URL is invalid", err)
	}
	if err := client.Test(ctx); err != nil {
		return mapWechatError(err)
	}
	return nil
}

// PushDraftToWechat copies one draft into the Official Account's draft box and,
// when publish is true, also submits it for publishing (freepublish/submit; the
// account must have that permission). cover is optional when a default cover is
// configured.
//
// Image prompts are turned into pictures only here, at push time. The prompt is
// rasterized locally (no image model); when this machine has no SVG-to-PNG
// converter a long article drops its images and placeholders, while a picture
// post can only be pushed if every picture was uploaded (images, keyed by the
// 1-based picture number; each is scaled to fit and re-encoded).
func (s *Service) PushDraftToWechat(ctx context.Context, id int64, publish bool, coverName string, cover []byte, images map[int][]byte) (*domain.MediaDraft, error) {
	s.pushMu.Lock()
	if _, busy := s.pushing[id]; busy {
		s.pushMu.Unlock()
		return nil, appError(CodeInvalidState, "this draft is already being pushed", nil)
	}
	s.pushing[id] = struct{}{}
	s.pushMu.Unlock()
	defer func() {
		s.pushMu.Lock()
		delete(s.pushing, id)
		s.pushMu.Unlock()
	}()

	draft, err := s.GetDraft(ctx, id)
	if err != nil {
		return nil, err
	}
	if draft.Status != domain.MediaDraftGenerated {
		return nil, appError(CodeInvalidState, "the draft is still being written or failed", nil)
	}
	settings, err := s.wechatSettings(ctx)
	if err != nil {
		return nil, err
	}
	if !settings.Configured() {
		return nil, appError(CodeWechatNotConfigured, wechat.ErrNotConfigured.Error(), nil)
	}
	publisher, err := s.wechatClientFor(settings)
	if err != nil {
		return nil, err
	}

	// Retrying a publish whose submit failed: the draft is already in the
	// draft box, so it is submitted again instead of added a second time.
	if publish && draft.WechatMediaID != "" && draft.WechatPublishID == "" {
		return s.submitPublish(ctx, publisher, draft.ID, draft.WechatMediaID)
	}

	var mediaID string
	switch draft.Format {
	case platforms.FormatGallery:
		if draft.Gallery == nil {
			return nil, appError(CodeInvalidState, "draft has no picture post content", nil)
		}
		mediaID, err = s.pushGallery(ctx, publisher, draft, images)
	case platforms.FormatArticle:
		if draft.Article == nil {
			return nil, appError(CodeInvalidState, "draft has no article content", nil)
		}
		mediaID, err = s.pushArticle(ctx, publisher, draft, settings, coverName, cover)
	default:
		return nil, appError(CodeInvalidState, "the draft has an unknown format", nil)
	}
	if err != nil {
		return nil, err
	}
	if err := s.repo.MarkDraftPushed(ctx, draft.ID, mediaID); err != nil {
		return nil, mapRepositoryError(err, CodeMediaDraftNotFound)
	}
	s.log.InfoContext(ctx, "media draft pushed to wechat draft box", "draft_id", draft.ID, "format", draft.Format)
	if publish {
		return s.submitPublish(ctx, publisher, draft.ID, mediaID)
	}
	return s.GetDraft(ctx, draft.ID)
}

// submitPublish submits a draft already in the draft box for publishing. The
// draft stays safe in the draft box; a failed submit is reported as such, with
// the WeChat error, and the owner can retry.
func (s *Service) submitPublish(ctx context.Context, publisher *wechat.Client, draftID int64, mediaID string) (*domain.MediaDraft, error) {
	publishID, err := publisher.SubmitPublish(ctx, mediaID)
	if err != nil {
		wrapped := mapWechatError(err)
		var appErr *AppError
		if errors.As(wrapped, &appErr) {
			appErr.Message = "the draft is in the draft box, but publishing failed (you can retry): " + appErr.Message
		}
		return nil, wrapped
	}
	if err := s.repo.MarkDraftPublished(ctx, draftID, publishID); err != nil {
		return nil, mapRepositoryError(err, CodeMediaDraftNotFound)
	}
	s.log.InfoContext(ctx, "media draft submitted for publishing", "draft_id", draftID)
	return s.GetDraft(ctx, draftID)
}

// pushGallery uploads every picture as a permanent material and creates a
// newspic draft. The first image is the cover, as the platform defines it. A
// slot with an uploaded file uses it (scaled to fit, as JPEG); the others are
// rendered as local title cards, which needs a local SVG converter.
func (s *Service) pushGallery(ctx context.Context, publisher *wechat.Client, draft *domain.MediaDraft, uploads map[int][]byte) (string, error) {
	gallery := draft.Gallery
	for slot, data := range uploads {
		if slot < 1 || slot > len(gallery.Images) {
			return "", appError(CodeInvalidInput, fmt.Sprintf("image%d does not match a picture of this post", slot), nil)
		}
		if len(data) > wechatCoverMaxBytes {
			return "", appError(CodeFileTooLarge, fmt.Sprintf("image %d must not exceed 10 MB", slot), nil)
		}
	}
	if len(uploads) < len(gallery.Images) {
		if ok, _ := imaging.Available(); !ok {
			return "", appError(CodeInvalidState,
				"this machine has no SVG-to-PNG converter (install ImageMagick, librsvg or Inkscape): upload every picture yourself, or a picture post cannot be pushed", imaging.ErrNoRasterizer)
		}
	}
	ids := make([]string, 0, len(gallery.Images))
	for i := range gallery.Images {
		var (
			name string
			data []byte
			err  error
		)
		if upload, ok := uploads[i+1]; ok {
			if data, err = imaging.FitJPEG(upload); err != nil {
				return "", appError(CodeUnsupportedFormat, fmt.Sprintf("image %d must be a JPEG, PNG or GIF picture", i+1), err)
			}
			name = fmt.Sprintf("clovergo-%d-%d.jpg", draft.ID, i+1)
		} else {
			if data, err = imaging.Render(gallery.Title, i+1, len(gallery.Images)); err != nil {
				return "", appError(CodeInternal, fmt.Sprintf("picture %d could not be rendered", i+1), err)
			}
			name = fmt.Sprintf("clovergo-%d-%d.png", draft.ID, i+1)
		}
		mediaID, err := publisher.UploadImage(ctx, name, data)
		if err != nil {
			return "", mapWechatError(err)
		}
		ids = append(ids, mediaID)
	}
	mediaID, err := publisher.AddGalleryDraft(ctx, gallery.Title, realBreaks(gallery.Caption), ids)
	if err != nil {
		return "", mapWechatError(err)
	}
	s.log.InfoContext(ctx, "picture post images uploaded", "draft_id", draft.ID, "image_count", len(ids), "uploaded", len(uploads))
	return mediaID, nil
}

// pushArticle uploads a cover and any chapter images, then creates a news
// draft. When no rasterizer exists the prompts are dropped silently: the
// article is still publishable as text, and no placeholder is left behind.
func (s *Service) pushArticle(ctx context.Context, publisher *wechat.Client, draft *domain.MediaDraft, settings domain.WechatSettings, coverName string, cover []byte) (string, error) {
	article := draft.Article
	thumb := settings.ThumbMediaID
	if len(cover) > 0 {
		if len(cover) > wechatCoverMaxBytes {
			return "", appError(CodeFileTooLarge, "cover image must not exceed 10 MB", nil)
		}
		fitted, err := imaging.FitJPEG(cover)
		if err != nil {
			return "", appError(CodeUnsupportedFormat, "cover image must be a JPEG, PNG or GIF picture", err)
		}
		uploaded, err := publisher.UploadImage(ctx, fmt.Sprintf("clovergo-%d-cover.jpg", draft.ID), fitted)
		if err != nil {
			return "", mapWechatError(err)
		}
		thumb = uploaded
	}

	rasterize, converter := imaging.Available()
	if rasterize && len(cover) == 0 && strings.TrimSpace(article.CoverImagePrompt) != "" {
		png, err := imaging.Render(article.Title, 1, 1)
		if err != nil {
			return "", appError(CodeInternal, "the cover image could not be rendered", err)
		}
		uploaded, err := publisher.UploadImage(ctx, fmt.Sprintf("clovergo-%d-cover.png", draft.ID), png)
		if err != nil {
			return "", mapWechatError(err)
		}
		thumb = uploaded
	}
	if thumb == "" {
		return "", appError(CodeInvalidInput, "a cover image is required (or set a default cover in the settings)", nil)
	}

	// Chapter images are inserted into the HTML with the URL the platform
	// returns; a prompt without a rasterizer simply disappears.
	inline := make(map[int]string)
	if rasterize {
		for _, section := range article.Sections {
			if strings.TrimSpace(section.ImagePrompt) == "" {
				continue
			}
			png, err := imaging.Render(section.Heading, section.Order, len(article.Sections))
			if err != nil {
				return "", appError(CodeInternal, fmt.Sprintf("image for chapter %d could not be rendered", section.Order), err)
			}
			url, err := publisher.UploadContentImage(ctx, fmt.Sprintf("clovergo-%d-section-%d.png", draft.ID, section.Order), png)
			if err != nil {
				return "", mapWechatError(err)
			}
			inline[section.Order] = url
		}
	}

	digest := article.Summary
	if utf8.RuneCountInString(digest) > wechatDigestMaxRunes {
		digest = string([]rune(digest)[:wechatDigestMaxRunes])
	}
	mediaID, err := publisher.AddDraft(ctx, wechat.Article{
		Title: article.Title, Author: settings.Author, Digest: digest,
		Content: RenderDraftHTML(*article, inline), ThumbMediaID: thumb,
	})
	if err != nil {
		return "", mapWechatError(err)
	}
	if len(inline) > 0 {
		s.log.InfoContext(ctx, "article images generated", "draft_id", draft.ID, "image_count", len(inline), "converter", converter)
	}
	return mediaID, nil
}

// Inline styles are the only formatting the WeChat editor reliably keeps: it
// drops bare <p>/<h2> styling, which is why an unstyled push reads as one
// undifferentiated block. Every node therefore carries its own margins.
const (
	wechatBodyStyle    = "margin:0 0 18px;font-size:16px;line-height:1.75;color:#333;letter-spacing:.5px;text-align:justify;"
	wechatHeadingStyle = "margin:34px 0 14px;font-size:19px;font-weight:700;line-height:1.5;color:#111;"
	wechatNoteStyle    = "margin:22px 0;text-align:center;font-size:13px;line-height:1.6;color:#999;"
)

// realBreaks repairs a draft written before literal "\n" sequences were
// normalized at generation time, so an old draft still pushes as paragraphs.
func realBreaks(text string) string {
	text = strings.ReplaceAll(text, `\r\n`, "\n")
	text = strings.ReplaceAll(text, `\n`, "\n")
	return strings.ReplaceAll(text, "\r\n", "\n")
}

// RenderDraftHTML is the article body as the WeChat editor stores it. images
// maps a chapter's order to the URL of the picture generated at push time; a
// chapter without an entry simply has no image.
func RenderDraftHTML(article domain.ArticleContent, images map[int]string) string {
	var b strings.Builder
	paragraphs := func(text string) {
		for _, block := range strings.Split(realBreaks(text), "\n\n") {
			if block = strings.TrimSpace(block); block == "" {
				continue
			}
			lines := strings.Split(block, "\n")
			for i, line := range lines {
				lines[i] = html.EscapeString(strings.TrimSpace(line))
			}
			fmt.Fprintf(&b, `<p style="%s">%s</p>`, wechatBodyStyle, strings.Join(lines, "<br/>"))
		}
	}
	paragraphs(article.Lead)
	for _, section := range article.Sections {
		fmt.Fprintf(&b, `<p style="%s">%s</p>`, wechatHeadingStyle, html.EscapeString(section.Heading))
		paragraphs(section.Body)
		if url := images[section.Order]; url != "" {
			fmt.Fprintf(&b, `<p style="margin:20px 0;text-align:center;"><img src="%s" style="max-width:100%%;border-radius:8px;" alt=""/></p>`,
				html.EscapeString(url))
			if strings.TrimSpace(section.ImageCaption) != "" {
				fmt.Fprintf(&b, `<p style="%s">%s</p>`, wechatNoteStyle, html.EscapeString(section.ImageCaption))
			}
		}
	}
	paragraphs(article.Conclusion)
	return b.String()
}

func mapWechatError(err error) error {
	if errors.Is(err, wechat.ErrNotConfigured) {
		return appError(CodeWechatNotConfigured, err.Error(), err)
	}
	return appError(CodeWechatUpstream, err.Error(), err)
}
