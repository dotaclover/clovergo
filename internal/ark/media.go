package ark

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"clovergo/internal/domain"
	"clovergo/internal/platforms"
)

const maxMediaKnowledge = 40

// maxMediaDocuments bounds how many raw materials a single draft may be
// written from. One is the normal case; the cap only keeps a request from
// turning into the whole library.
const maxMediaDocuments = 5

type mediaKnowledgeContext struct {
	ID       int64             `json:"knowledge_id"`
	Kind     string            `json:"kind"`
	Title    string            `json:"title"`
	Body     string            `json:"body"`
	Tags     []string          `json:"tags"`
	Metadata json.RawMessage   `json:"metadata"`
	Evidence []mediaQuoteEntry `json:"evidence"`
}

type mediaQuoteEntry struct {
	DocumentName string `json:"document_name"`
	Quote        string `json:"quote"`
}

// mediaDocumentContext is a raw material handed over as-is. A material is not
// knowledge: it may be a plain archive, so its whole stored text is the source
// the draft has to stay grounded in.
type mediaDocumentContext struct {
	DocumentID   int64  `json:"document_id"`
	DocumentName string `json:"document_name"`
	Topic        string `json:"topic"`
	Text         string `json:"text"`
}

// mediaContext validates the knowledge sent to Ark. Zero items is allowed when
// the draft is written from raw materials instead.
func mediaContext(knowledge []domain.KnowledgeItem) ([]mediaKnowledgeContext, error) {
	if len(knowledge) > maxMediaKnowledge {
		return nil, fmt.Errorf("at most %d knowledge items can be used", maxMediaKnowledge)
	}
	items := make([]mediaKnowledgeContext, 0, len(knowledge))
	seen := make(map[int64]struct{}, len(knowledge))
	for _, item := range knowledge {
		if item.ID <= 0 {
			return nil, fmt.Errorf("knowledge item %d is not a stored item", item.ID)
		}
		if _, duplicate := seen[item.ID]; duplicate {
			return nil, fmt.Errorf("knowledge item %d is duplicated", item.ID)
		}
		if len(item.Evidence) == 0 {
			return nil, fmt.Errorf("knowledge item %d has no evidence", item.ID)
		}
		seen[item.ID] = struct{}{}
		quotes := make([]mediaQuoteEntry, 0, len(item.Evidence))
		for _, evidence := range item.Evidence {
			quotes = append(quotes, mediaQuoteEntry{DocumentName: evidence.DocumentName, Quote: evidence.Quote})
		}
		items = append(items, mediaKnowledgeContext{
			ID: item.ID, Kind: item.Kind, Title: item.Title, Body: item.Body,
			Tags: item.Tags, Metadata: item.Metadata, Evidence: quotes,
		})
	}
	return items, nil
}

// mediaDocumentContexts validates the raw materials sent to Ark.
func mediaDocumentContexts(documents []domain.SourceDocument) ([]mediaDocumentContext, error) {
	if len(documents) > maxMediaDocuments {
		return nil, fmt.Errorf("at most %d materials can be used", maxMediaDocuments)
	}
	items := make([]mediaDocumentContext, 0, len(documents))
	seen := make(map[int64]struct{}, len(documents))
	for _, document := range documents {
		if document.DocumentID <= 0 {
			return nil, fmt.Errorf("material %d is not a stored material", document.DocumentID)
		}
		if _, duplicate := seen[document.DocumentID]; duplicate {
			return nil, fmt.Errorf("material %d is duplicated", document.DocumentID)
		}
		if strings.TrimSpace(document.Text) == "" {
			return nil, fmt.Errorf("material %d has no text", document.DocumentID)
		}
		seen[document.DocumentID] = struct{}{}
		items = append(items, mediaDocumentContext{
			DocumentID: document.DocumentID, DocumentName: document.DocumentName,
			Topic: document.Topic, Text: document.Text,
		})
	}
	return items, nil
}

// DraftForPlatform writes one draft in the platform's format. The composed
// system prompt carries the style and limits; the tool schema and validation
// enforce the same limits on the output. A draft is grounded in knowledge, in
// raw materials, or in both.
func (c *Client) DraftForPlatform(
	ctx context.Context,
	platform platforms.Platform,
	system string,
	input domain.MediaDraftInput,
	knowledge []domain.KnowledgeItem,
	documents []domain.SourceDocument,
) (domain.MediaDraftBody, domain.TokenUsage, error) {
	if strings.TrimSpace(system) == "" {
		return domain.MediaDraftBody{}, domain.TokenUsage{}, fmt.Errorf("draft system prompt is empty")
	}
	if strings.TrimSpace(input.Topic) == "" {
		return domain.MediaDraftBody{}, domain.TokenUsage{}, fmt.Errorf("draft topic is required")
	}
	// No source at all is allowed: an inspiration on its own can carry a draft.
	// When a source is supplied, the prompt makes the facts come from it.
	contextItems, err := mediaContext(knowledge)
	if err != nil {
		return domain.MediaDraftBody{}, domain.TokenUsage{}, err
	}
	documentItems, err := mediaDocumentContexts(documents)
	if err != nil {
		return domain.MediaDraftBody{}, domain.TokenUsage{}, err
	}
	requestJSON, err := json.Marshal(struct {
		Topic     string                  `json:"topic"`
		Angle     string                  `json:"angle"`
		Audience  string                  `json:"audience"`
		Knowledge []mediaKnowledgeContext `json:"confirmed_knowledge"`
		Documents []mediaDocumentContext  `json:"source_documents"`
	}{Topic: input.Topic, Angle: input.Angle, Audience: input.Audience,
		Knowledge: contextItems, Documents: documentItems})
	if err != nil {
		return domain.MediaDraftBody{}, domain.TokenUsage{}, fmt.Errorf("encode draft context: %w", err)
	}

	format := input.Format
	if format == "" {
		format = platform.Format
	}
	if !platforms.SupportsFormat(format) {
		return domain.MediaDraftBody{}, domain.TokenUsage{}, fmt.Errorf("platform %s has unsupported format %s", platform.ID, format)
	}
	data := "Write the draft from this application data:\n" + string(requestJSON)
	if format == platforms.FormatGallery {
		wantImages := input.ImageCount
		if wantImages == 0 {
			wantImages = platforms.GalleryImageDefaultCount
		}
		data += fmt.Sprintf("\nThe images array must contain exactly %d item(s).", wantImages)
		arguments, usage, err := c.callTool(ctx, system, data, galleryTool())
		if err != nil {
			return domain.MediaDraftBody{}, usage, err
		}
		var raw struct {
			Title   string   `json:"title"`
			Caption string   `json:"caption"`
			Images  []string `json:"images"`
		}
		if err := json.Unmarshal(arguments, &raw); err != nil {
			return domain.MediaDraftBody{}, usage, fmt.Errorf("%w: gallery arguments do not match the schema: %v", ErrInvalidResponse, err)
		}
		gallery := domain.GalleryContent{Title: raw.Title, Caption: raw.Caption}
		for i, prompt := range raw.Images {
			gallery.Images = append(gallery.Images, domain.GalleryImage{Order: i + 1, Prompt: prompt})
		}
		if err := validateGallery(&gallery); err != nil {
			return domain.MediaDraftBody{}, usage, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
		}
		if len(gallery.Images) != wantImages {
			return domain.MediaDraftBody{}, usage, fmt.Errorf("%w: expected %d images, got %d", ErrInvalidResponse, wantImages, len(gallery.Images))
		}
		return domain.MediaDraftBody{Gallery: &gallery}, usage, nil
	}
	arguments, usage, err := c.callTool(ctx, system, data, articleTool())
	if err != nil {
		return domain.MediaDraftBody{}, usage, err
	}
	var body domain.MediaDraftBody
	var article domain.ArticleContent
	if err := json.Unmarshal(arguments, &article); err != nil {
		return domain.MediaDraftBody{}, usage, fmt.Errorf("%w: article arguments do not match the schema: %v", ErrInvalidResponse, err)
	}
	if err := validateArticle(&article); err != nil {
		return domain.MediaDraftBody{}, usage, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	body.Article = &article
	return body, usage, nil
}

// normalizeBreaks turns the two-character sequences \n and \r\n that a model
// sometimes emits as literal text into real line breaks, so paragraphs stay
// paragraphs in the editor and in the pushed HTML instead of showing "\n".
func normalizeBreaks(value string) string {
	value = strings.ReplaceAll(value, `\r\n`, "\n")
	value = strings.ReplaceAll(value, `\n`, "\n")
	return strings.ReplaceAll(value, "\r\n", "\n")
}

func requireText(name, value string, maxRunes int) error {
	if value == "" {
		return fmt.Errorf("%s is empty", name)
	}
	if utf8.RuneCountInString(value) > maxRunes {
		return fmt.Errorf("%s exceeds %d characters", name, maxRunes)
	}
	return nil
}

// cleanTags trims and strips leading # signs and drops duplicates.
func cleanTags(name string, values []string, max int) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(value), "#＃"))
		if err := requireText(name, value, platforms.ArticleTagMaxRunes); err != nil {
			return nil, err
		}
		if _, duplicate := seen[value]; !duplicate {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	if len(result) == 0 || len(result) > max {
		return nil, fmt.Errorf("%s must contain 1-%d items", name, max)
	}
	return result, nil
}

// ValidateArticle exposes the same rules that model output has to satisfy.
// An owner's edit is put through it too, so a hand-written change can never be
// in a shape the generator would refuse to produce.
func ValidateArticle(article *domain.ArticleContent) error {
	return validateArticle(article)
}

// validateArticle is the single place that decides what a usable WeChat
// Official Account article looks like. It runs on both model output and
// owner-written edits, so the two can never drift apart.
func validateArticle(article *domain.ArticleContent) error {
	article.Title = strings.TrimSpace(article.Title)
	article.Summary = normalizeBreaks(strings.TrimSpace(article.Summary))
	article.Lead = normalizeBreaks(strings.TrimSpace(article.Lead))
	article.CoverImagePrompt = strings.TrimSpace(article.CoverImagePrompt)
	article.Conclusion = normalizeBreaks(strings.TrimSpace(article.Conclusion))
	if err := requireText("title", article.Title, platforms.ArticleTitleMaxRunes); err != nil {
		return err
	}
	if err := requireText("summary", article.Summary, platforms.ArticleSummaryMaxRunes); err != nil {
		return err
	}
	if err := requireText("lead", article.Lead, platforms.ArticleLeadMaxRunes); err != nil {
		return err
	}
	if err := requireText("cover_image_prompt", article.CoverImagePrompt, platforms.ArticleImagePromptMaxRunes); err != nil {
		return err
	}
	if err := requireText("conclusion", article.Conclusion, platforms.ArticleConclusionMaxRunes); err != nil {
		return err
	}
	if len(article.Sections) < platforms.ArticleSectionMinCount || len(article.Sections) > platforms.ArticleSectionMaxCount {
		return fmt.Errorf("sections must contain %d-%d items", platforms.ArticleSectionMinCount, platforms.ArticleSectionMaxCount)
	}
	for i := range article.Sections {
		section := &article.Sections[i]
		section.Heading = normalizeBreaks(strings.TrimSpace(section.Heading))
		section.Body = normalizeBreaks(strings.TrimSpace(section.Body))
		section.ImagePrompt = strings.TrimSpace(section.ImagePrompt)
		section.ImageCaption = strings.TrimSpace(section.ImageCaption)
		if section.Order != i+1 {
			return fmt.Errorf("section orders must be consecutive starting at 1")
		}
		if err := requireText("sections.heading", section.Heading, platforms.ArticleHeadingMaxRunes); err != nil {
			return err
		}
		if err := requireText("sections.body", section.Body, platforms.ArticleSectionMaxRunes); err != nil {
			return err
		}
		// The image is the one optional part: a chapter is allowed to read
		// better without one, so an empty prompt is fine and is left empty.
		// A caption only makes sense together with the image.
		if section.ImagePrompt != "" {
			if err := requireText("sections.image_prompt", section.ImagePrompt, platforms.ArticleImagePromptMaxRunes); err != nil {
				return err
			}
			if err := requireText("sections.image_caption", section.ImageCaption, platforms.ArticleImageCaptionMaxRunes); err != nil {
				return err
			}
		} else {
			section.ImageCaption = ""
		}
	}
	// The per-field caps allow a piece far longer than anyone reads. The whole
	// prose — lead, every chapter and the conclusion — has one budget, and it
	// is the limit the page shows.
	totalRunes := utf8.RuneCountInString(article.Lead) + utf8.RuneCountInString(article.Conclusion)
	for _, section := range article.Sections {
		totalRunes += utf8.RuneCountInString(section.Body)
	}
	if totalRunes > platforms.ArticleTotalMaxRunes {
		return fmt.Errorf("the article is %d characters; lead, chapters and conclusion together must not exceed %d", totalRunes, platforms.ArticleTotalMaxRunes)
	}
	tags, err := cleanTags("tags", article.Tags, platforms.ArticleMaxTags)
	article.Tags = tags
	return err
}

// ValidateGallery exposes the picture post's rules for owner edits, so an
// edited post is checked exactly like generated output.
func ValidateGallery(gallery *domain.GalleryContent) error {
	return validateGallery(gallery)
}

// validateGallery is the one place deciding what a usable picture post is.
func validateGallery(gallery *domain.GalleryContent) error {
	gallery.Title = strings.TrimSpace(gallery.Title)
	gallery.Caption = normalizeBreaks(strings.TrimSpace(gallery.Caption))
	if err := requireText("title", gallery.Title, platforms.GalleryTitleMaxRunes); err != nil {
		return err
	}
	if err := requireText("caption", gallery.Caption, platforms.GalleryCaptionMaxRunes); err != nil {
		return err
	}
	if len(gallery.Images) < platforms.GalleryImageMinCount || len(gallery.Images) > platforms.GalleryImageMaxCount {
		return fmt.Errorf("images must contain %d-%d items", platforms.GalleryImageMinCount, platforms.GalleryImageMaxCount)
	}
	for i := range gallery.Images {
		image := &gallery.Images[i]
		image.Order = i + 1
		image.Prompt = strings.TrimSpace(image.Prompt)
		if err := requireText("images.prompt", image.Prompt, platforms.GalleryImagePromptMaxRunes); err != nil {
			return err
		}
	}
	return nil
}

func stringProperty(maxRunes int) map[string]any {
	return map[string]any{"type": "string", "maxLength": maxRunes}
}

func stringArray(maxItems, maxRunes int) map[string]any {
	return map[string]any{"type": "array", "minItems": 1, "maxItems": maxItems, "items": stringProperty(maxRunes)}
}

// objectSchemaWhere marks only the named properties as required, so a field
// can be genuinely optional instead of being forced to an empty string.
func objectSchemaWhere(properties map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func galleryTool() toolDefinition {
	return toolDefinition{
		Name:        "submit_gallery_draft",
		Description: "Submit a grounded WeChat Official Account picture post draft",
		MaxTokens:   2048,
		Parameters: objectSchemaWhere(map[string]any{
			"title":   stringProperty(platforms.GalleryTitleMaxRunes),
			"caption": stringProperty(platforms.GalleryCaptionMaxRunes),
			"images": map[string]any{
				"type": "array", "minItems": platforms.GalleryImageMinCount, "maxItems": platforms.GalleryImageMaxCount,
				"items": stringProperty(platforms.GalleryImagePromptMaxRunes),
			},
		}, "title", "caption", "images"),
	}
}

func articleTool() toolDefinition {
	// image_prompt is deliberately absent from the section's required list:
	// the prompt tells the model to leave it out when a section reads better
	// without an image, and a required field would push it to invent one.
	section := objectSchemaWhere(map[string]any{
		"order":         map[string]any{"type": "integer", "minimum": 1, "maximum": platforms.ArticleSectionMaxCount},
		"heading":       stringProperty(platforms.ArticleHeadingMaxRunes),
		"body":          stringProperty(platforms.ArticleSectionMaxRunes),
		"image_prompt":  stringProperty(platforms.ArticleImagePromptMaxRunes),
		"image_caption": stringProperty(platforms.ArticleImageCaptionMaxRunes),
	}, "order", "heading", "body")
	return toolDefinition{
		Name:        "submit_article_draft",
		Description: "Submit a grounded WeChat Official Account article draft",
		// The prose budget is 2000 characters total, so 4096 tokens leaves room
		// for the JSON envelope without letting a runaway response run to the
		// server default. Truncation is reported as ErrInvalidResponse, which
		// the caller retries once.
		MaxTokens: 4096,
		Parameters: objectSchemaWhere(map[string]any{
			"title":              stringProperty(platforms.ArticleTitleMaxRunes),
			"summary":            stringProperty(platforms.ArticleSummaryMaxRunes),
			"lead":               stringProperty(platforms.ArticleLeadMaxRunes),
			"cover_image_prompt": stringProperty(platforms.ArticleImagePromptMaxRunes),
			"sections": map[string]any{
				"type": "array", "minItems": platforms.ArticleSectionMinCount, "maxItems": platforms.ArticleSectionMaxCount, "items": section,
			},
			"conclusion": stringProperty(platforms.ArticleConclusionMaxRunes),
			"tags":       stringArray(platforms.ArticleMaxTags, platforms.ArticleTagMaxRunes),
		}, "title", "summary", "lead", "cover_image_prompt", "sections", "conclusion", "tags"),
	}
}
