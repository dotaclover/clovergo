package platforms

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Content formats a platform draft can take. Formats are code: each has its
// own tool schema, validation, and renderer.
//
// The Official Account has two shapes: a long-form article (news) and a
// picture post (newspic, the "小绿书" feed), which is a short caption under a
// set of images.
const (
	FormatArticle = "article"
	FormatGallery = "gallery"
)

// Formats returns the formats a platform can write, in display order.
func Formats() []string { return []string{FormatArticle, FormatGallery} }

// SupportsFormat reports whether format is one this build can generate.
func SupportsFormat(format string) bool {
	return format == FormatArticle || format == FormatGallery
}

// GalleryRules returns the picture post's rules. Like the article's, they are
// constants: the model, the validator and the page all read these numbers.
func GalleryRules() []FormatRule {
	return []FormatRule{
		{"标题", fmt.Sprintf("最多 %d 字", GalleryTitleMaxRunes)},
		{"正文", fmt.Sprintf("最多 %d 字，纯文本", GalleryCaptionMaxRunes)},
		{"图片", fmt.Sprintf("%d–%d 张（默认 %d 张），每张一条画面提示词；推送时每张可改用自己上传的图（自动调整尺寸），未上传的由本机生成只含标题的卡片图（提示词不会印在图上）", GalleryImageMinCount, GalleryImageMaxCount, GalleryImageDefaultCount)},
		{"图片提示词", fmt.Sprintf("每条最多 %d 字，描述画面，不要出现文字", GalleryImagePromptMaxRunes)},
	}
}

const (
	Gongzhonghao = "gongzhonghao"
)

// The article's rules. They are properties of the format, not preferences, so
// they are constants rather than owner settings: the owner shapes the voice in
// the style layer, and the numbers stay put. They live here so the prompt
// text, the validator and the UI all read the same number and cannot drift.
const (
	// ArticleTitleMaxRunes is well inside WeChat's own 64-character title
	// limit; 30 keeps a title scannable in a chat share.
	ArticleTitleMaxRunes = 30
	// ArticleSummaryMaxRunes is WeChat's own digest limit; longer text is
	// truncated in the share card.
	ArticleSummaryMaxRunes = 120
	ArticleLeadMaxRunes    = 300
	// ArticleSectionMinCount / ArticleSectionMaxCount bound the chapters. Two
	// is the least that still reads as a developed article; past five the
	// chapters turn into a list, which is not what this format is for.
	ArticleSectionMinCount = 2
	ArticleSectionMaxCount = 5
	ArticleHeadingMaxRunes = 40
	// ArticleSectionMaxRunes caps one chapter's prose. A chapter that cannot
	// be said in 800 characters is really two chapters.
	ArticleSectionMaxRunes = 800
	// ArticleTotalTargetRunes / ArticleTotalMaxRunes bound the prose of the
	// whole article — lead, every section body and the conclusion together. A
	// thousand characters is what this writer is asked for; past two thousand
	// the piece stops being read, and the single model call gets long enough to
	// risk a malformed tool response.
	ArticleTotalTargetRunes     = 1000
	ArticleTotalMaxRunes        = 2000
	ArticleConclusionMaxRunes   = 400
	ArticleImagePromptMaxRunes  = 500
	ArticleImageCaptionMaxRunes = 60
	ArticleTagMaxRunes          = 20
	ArticleMaxTags              = 5

	// Picture post ("小绿书") limits. The platform accepts 1-20 images; an edit
	// may keep just one, while a generated set stays below the cap so every
	// card carries a real idea.
	GalleryTitleMaxRunes       = 30
	GalleryCaptionMaxRunes     = 500
	GalleryImageMinCount       = 1
	GalleryImageMaxCount       = 3
	GalleryImageDefaultCount   = 1
	GalleryImagePromptMaxRunes = 300

	// StyleRulesMaxRunes caps the owner's style layer.
	StyleRulesMaxRunes = 3000
)

// FormatRule is one displayable line of the article's rules. The server owns
// the numbers, so the page renders them instead of repeating them in the
// frontend, where they would quietly drift from what is enforced.
type FormatRule struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// ArticleRules returns the article's rules in the order the page shows them.
func ArticleRules() []FormatRule {
	return []FormatRule{
		{"标题", fmt.Sprintf("最多 %d 字", ArticleTitleMaxRunes)},
		{"摘要", fmt.Sprintf("最多 %d 字，分享卡片上显示的就是它", ArticleSummaryMaxRunes)},
		{"引言", fmt.Sprintf("最多 %d 字，开篇铺陈，不要一上来就列点", ArticleLeadMaxRunes)},
		{"全文", fmt.Sprintf("目标 %d 字左右，最多 %d 字（引言 + 各章正文 + 结尾合计）", ArticleTotalTargetRunes, ArticleTotalMaxRunes)},
		{"正文", fmt.Sprintf("%d–%d 章，每章最多 %d 字", ArticleSectionMinCount, ArticleSectionMaxCount, ArticleSectionMaxRunes)},
		{"小标题", fmt.Sprintf("每章一个，最多 %d 字", ArticleHeadingMaxRunes)},
		{"结尾", fmt.Sprintf("最多 %d 字，收束全文", ArticleConclusionMaxRunes)},
		{"配图", fmt.Sprintf("可选；填了配图就要填图注，图注最多 %d 字", ArticleImageCaptionMaxRunes)},
		{"标签", fmt.Sprintf("最多 %d 个，不加 # 号", ArticleMaxTags)},
	}
}

var (
	ErrUnknownPlatform = errors.New("unknown platform")
	ErrInvalidOverride = errors.New("invalid platform style")
)

// Settings is the one part of a platform the owner shapes: the style layer of
// the draft prompt. Every number is a format constant above, so there is
// nothing here to misconfigure.
type Settings struct {
	StyleRules string `json:"style_rules"`
}

// Platform is a self-media target with its effective settings.
type Platform struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Format      string   `json:"format"`
	Formats     []string `json:"formats"`
	Description string   `json:"description"`
	Settings
	Revision  int      `json:"revision"`
	IsDefault bool     `json:"is_default"`
	Defaults  Settings `json:"defaults"`
	// Rules describes the article format; GalleryRules describes the picture
	// post. Both are read-only: the numbers are constants above, and the owner
	// shapes the voice instead.
	Rules        []FormatRule `json:"rules"`
	GalleryRules []FormatRule `json:"gallery_rules,omitempty"`
	// StyleRulesMax is the editor's limit, sent so the page does not keep its
	// own copy of the number.
	StyleRulesMax int `json:"style_rules_max"`
	// GalleryImageMax/Default bound the picture post's image count picker.
	GalleryImageMax     int `json:"gallery_image_max"`
	GalleryImageDefault int `json:"gallery_image_default"`
}

var defaults = []Platform{
	{
		ID: Gongzhonghao, Name: "公众号", Format: FormatArticle, Formats: Formats(),
		Description: "两种形态：长图文文章（标题、摘要、引言、分章正文与结尾），或图文帖（短正文 + 一组图片）。",
		Settings: Settings{
			StyleRules: `平台：微信公众号文章。
为普通读者写一篇简短完整的文章：信息扎实、有展开、值得读到结尾。宁可短而完整，不要长而注水。
标题直接说明这篇讲什么，不做标题党，不承诺效果。
摘要用一两句话说清读者能带走什么；它会成为分享卡片上的文字。
引言负责开篇：把读者放进这篇文章要讲的处境里，或者点出它回答的问题，写两三个自然段。不要一上来就列点。
正文分章。每章有一个点明主旨的小标题，下面用几段话展开论述、举例或说明边界。写连贯的散文，不要条目式罗列。
结尾收束全文：换一种说法重述要点，或者告诉读者接下来可以做什么。
正文是纯文本，不是 Markdown：作者会把它粘进公众号编辑器，所以不要出现 #、*、-、** 这类记号。
配图要克制。某一章确实需要配图时，给一条干净的示意图配图提示词（画面里不要出现文字），并配一句图注；不需要配图就都留空。
标签是普通词，不加 # 号。`,
		},
	},
}

// Default returns the built-in definition of a platform.
func Default(id string) (Platform, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, platform := range defaults {
		if platform.ID == id {
			platform.Defaults = platform.Settings
			platform.IsDefault = true
			platform.Rules = ArticleRules()
			platform.GalleryRules = GalleryRules()
			platform.StyleRulesMax = StyleRulesMaxRunes
			platform.GalleryImageMax = GalleryImageMaxCount
			platform.GalleryImageDefault = GalleryImageDefaultCount
			return platform, nil
		}
	}
	return Platform{}, ErrUnknownPlatform
}

func IDs() []string {
	ids := make([]string, 0, len(defaults))
	for _, platform := range defaults {
		ids = append(ids, platform.ID)
	}
	return ids
}

// Apply returns the platform with owner settings at revision applied.
func Apply(platform Platform, settings Settings, revision int) Platform {
	platform.Settings = settings
	platform.Revision = revision
	platform.IsDefault = settings == platform.Defaults
	return platform
}

// Validate checks the one owner-editable field. Everything numeric is a
// format constant, so there is nothing else to get wrong.
func Validate(platform Platform, settings Settings) (Settings, error) {
	settings.StyleRules = strings.TrimSpace(settings.StyleRules)
	if settings.StyleRules == "" || utf8.RuneCountInString(settings.StyleRules) > StyleRulesMaxRunes {
		return Settings{}, fmt.Errorf("%w: style_rules 必填，且不超过 %d 字", ErrInvalidOverride, StyleRulesMaxRunes)
	}
	return settings, nil
}
