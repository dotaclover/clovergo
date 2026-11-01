package prompts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"clovergo/internal/domain"
	"clovergo/internal/platforms"
)

// Layer sources.
const (
	SourceCore     = "core"
	SourcePlatform = "platform"
	SourceFormat   = "format"
)

// Layer is one part of a composed system prompt.
type Layer struct {
	// Key names the editable layer, empty for generated or per-call layers.
	Key      string `json:"key,omitempty"`
	Name     string `json:"name"`
	Source   string `json:"source"`
	Editable bool   `json:"editable"`
	// EditTarget names where an editable layer is changed: "prompt:<key>" for
	// an owner override, "platform:<id>" for a platform's style rules.
	EditTarget *string `json:"edit_target"`
	Text       string  `json:"text"`
}

// Composed is the full system prompt for one model call, with its layers and
// a version string recorded on runs and drafts.
type Composed struct {
	Call    string  `json:"call"`
	Title   string  `json:"title"`
	Version string  `json:"version"`
	SHA256  string  `json:"sha256"`
	Text    string  `json:"text"`
	Layers  []Layer `json:"layers"`
}

// PromptVersion is the short form stored on artifacts: the layer versions plus
// the first 12 hex digits of the composed text's hash.
func (c Composed) PromptVersion() string {
	return c.Version + "#" + c.SHA256[:12]
}

// Override is the owner's replacement text for one built-in layer.
type Override struct {
	Text     string
	Revision int
}

// Overrides maps a layer key to its replacement. An empty map composes the
// built-in text exactly as compiled in. Overrides never bypass validation: a
// replaced layer can ask for anything, and internal/ark and internal/service
// still reject output that breaks the code rules.
type Overrides map[string]Override

func (o Overrides) text(key string) (string, bool) {
	if override, ok := o[key]; ok && strings.TrimSpace(override.Text) != "" {
		return override.Text, true
	}
	return coreText(key), false
}

func (o Overrides) overrideVersion(key string) string {
	if override, ok := o[key]; ok && strings.TrimSpace(override.Text) != "" {
		return fmt.Sprintf("prompt:%s@r%d", key, override.Revision)
	}
	return ""
}

func (o Overrides) versions(keys ...string) []string {
	parts := []string{CoreVersion}
	for _, key := range keys {
		if version := o.overrideVersion(key); version != "" {
			parts = append(parts, version)
		}
	}
	return parts
}

// layer composes one built-in layer, marking it editable when the owner has
// replaced it.
func (o Overrides) layer(key string) Layer {
	text, overridden := o.text(key)
	layer := Layer{Key: key, Name: coreName(key), Source: SourceCore, Text: text}
	if overridden {
		layer.Name += "（已微调）"
		layer.Editable = true
		target := "prompt:" + key
		layer.EditTarget = &target
	}
	return layer
}

func compose(call, title string, versions []string, layers ...Layer) Composed {
	parts := make([]string, 0, len(layers))
	for _, layer := range layers {
		if strings.TrimSpace(layer.Text) != "" {
			parts = append(parts, layer.Text)
		}
	}
	text := strings.Join(parts, "\n\n")
	sum := sha256.Sum256([]byte(text))
	return Composed{
		Call: call, Title: title, Version: strings.Join(versions, "|"), SHA256: hex.EncodeToString(sum[:]),
		Text: text, Layers: layers,
	}
}

// kindLayer is generated from domain.Kinds, the same table the extraction tool
// schema uses as its enum. One source of truth means the taxonomy in the
// prompt and the taxonomy the model is allowed to emit can never drift.
func kindLayer() Layer {
	var b strings.Builder
	b.WriteString("【分类】\nkind 必须从下列固定取值里选一个最贴切的，不得自造：")
	for _, kind := range domain.Kinds {
		fmt.Fprintf(&b, "\n- %s %s：%s", kind.ID, kind.Label, kind.Hint)
	}
	return Layer{Name: "分类", Source: SourceCore, Text: b.String()}
}

// Extraction composes the system prompt for knowledge extraction. It takes no
// configuration: the same prompt runs for every material in the library.
func Extraction(o Overrides) Composed {
	return compose("extract", "资料提炼", o.versions("extraction"),
		o.layer("extraction"),
		kindLayer(),
	)
}

// Summary composes the system prompt for the upload-time material summary.
func Summary(o Overrides) Composed {
	return compose("summary", "资料摘要", o.versions("summary"), o.layer("summary"))
}

// Answer composes the system prompt for grounded question answering.
func Answer(o Overrides) Composed {
	return compose("answer", "带引用问答", o.versions("answer"), o.layer("answer"))
}

// Agent composes the system prompt for the tool-calling agent.
func Agent(o Overrides) Composed {
	return compose("agent", "Agent", o.versions("agent"), o.layer("agent"))
}

// Draft composes the system prompt for one article draft. The draft is written
// from the platform style rules alone; no worked example is involved.
func Draft(platform platforms.Platform, o Overrides) Composed {
	target := "platform:" + platform.ID
	layers := []Layer{
		o.layer("media"),
		o.layer("persona"),
		{Name: "平台风格：" + platform.Name, Source: SourcePlatform, Editable: true, EditTarget: &target, Text: platform.StyleRules},
	}
	versions := o.versions("media", "persona")
	versions = append(versions, fmt.Sprintf("platform:%s@r%d", platform.ID, platform.Revision))
	layers = append(layers,
		Layer{Name: "格式约束", Source: SourceFormat, Text: FormatRules(platform)},
	)
	return compose("draft:"+platform.ID, platform.Name+"草稿", versions, layers...)
}

// Gallery composes the system prompt for one picture post. It uses the same
// writing core and persona, plus the picture-post layer; the article's platform
// style is not involved because it describes a long-form piece.
func Gallery(platform platforms.Platform, o Overrides) Composed {
	layers := []Layer{
		o.layer("media"),
		o.layer("gallery"),
		o.layer("persona"),
	}
	versions := o.versions("media", "gallery", "persona")
	versions = append(versions, fmt.Sprintf("platform:%s@r%d", platform.ID, platform.Revision))
	layers = append(layers,
		Layer{Name: "格式约束", Source: SourceFormat, Text: GalleryFormatRules()},
	)
	return compose("draft:"+platform.ID+":gallery", platform.Name+"图文帖", versions, layers...)
}

// GalleryFormatRules states the picture post's structure and limits.
func GalleryFormatRules() string {
	return fmt.Sprintf(`格式：这是一条公众号图文帖（图片消息 newspic），不是长文章。
title 最多 %d 字；caption 是正文，最多 %d 字，纯文本，不分章。
images 是 %d-%d 张图的画面提示词，按 1 起连续编号；每条最多 %d 字，只描述画面，不要出现文字、字母或数字。
每张图都必须是画面，不能是纯色块或空白；推送时未上传图片的位置只会生成印有标题的本机卡片，提示词与正文都不会写进图片。`,
		platforms.GalleryTitleMaxRunes, platforms.GalleryCaptionMaxRunes,
		platforms.GalleryImageMinCount, platforms.GalleryImageMaxCount,
		platforms.GalleryImagePromptMaxRunes)
}

// FormatRules states the article's structure and numeric limits in the same
// words the UI shows them. The numbers come from the format registry so
// prompt, validator and page can never disagree.
func FormatRules(platform platforms.Platform) string {
	if platform.Format != platforms.FormatArticle {
		return ""
	}
	return fmt.Sprintf(`格式：这是一篇微信公众号文章，不是信息流帖子。
全文（lead + 各 section 的 body + conclusion 合计）目标 %d 字左右，最多 %d 字；宁短勿长，超出上限的输出会被判为无效。
title 最多 %d 字；summary 最多 %d 字；lead 最多 %d 字；conclusion 最多 %d 字；cover_image_prompt 描述封面画面（不生成图片）。
%d-%d 个 section，按 1 起连续编号；每个 section 的 heading 最多 %d 字，body 最多 %d 字，且必须是纯文本，不能写 Markdown 记号。
每个 section 都有 order、heading、body；image_prompt 与 image_caption 可选——这一章不需要配图时两者都省略，需要配图时两者都给，caption 是图片下方那一行字。
标签 1-%d 个，普通词，不加 # 号。`,
		platforms.ArticleTotalTargetRunes, platforms.ArticleTotalMaxRunes,
		platforms.ArticleTitleMaxRunes, platforms.ArticleSummaryMaxRunes,
		platforms.ArticleLeadMaxRunes, platforms.ArticleConclusionMaxRunes,
		platforms.ArticleSectionMinCount, platforms.ArticleSectionMaxCount,
		platforms.ArticleHeadingMaxRunes, platforms.ArticleSectionMaxRunes,
		platforms.ArticleMaxTags)
}
