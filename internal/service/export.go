package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"clovergo/internal/domain"
	"clovergo/internal/platforms"
)

type zipFile struct {
	name string
	data []byte
}

func buildZIP(files []zipFile) ([]byte, error) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, file := range files {
		entry, err := writer.Create(file.name)
		if err != nil {
			_ = writer.Close()
			return nil, err
		}
		if _, err := entry.Write(file.data); err != nil {
			_ = writer.Close()
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func indentedJSON(value any) []byte {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return []byte("{}")
	}
	return append(data, '\n')
}

// ExportDraft packages a draft for manual publishing: a copy-ready Markdown
// file plus the structured draft. Sources are not exported: a draft does not
// cite them, and the article stands on its own.
func (s *Service) ExportDraft(ctx context.Context, id int64) ([]byte, string, error) {
	draft, err := s.GetDraft(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if draft.Status != domain.MediaDraftGenerated {
		return nil, "", appError(CodeInvalidState, "the draft is still being written or failed", nil)
	}
	manifest := map[string]any{
		"schema_version": "1", "media_draft_id": draft.ID, "artifact_id": draft.ArtifactID, "run_id": draft.RunID,
		"platform": draft.Platform, "format": draft.Format,
		"topic": draft.Topic, "angle": draft.Angle, "audience": draft.Audience, "model": draft.Model,
		"prompt_version": draft.PromptVersion, "created_at": draft.CreatedAt,
		"publishing": "manual", "media_assets": "generated_locally_at_push",
	}
	data, err := buildZIP([]zipFile{
		{"manifest.json", indentedJSON(manifest)},
		{"draft.md", []byte(RenderDraftMarkdown(*draft))},
		{"draft.json", indentedJSON(draft)},
	})
	if err != nil {
		return nil, "", appError(CodeInternal, "export media draft failed", err)
	}
	return data, fmt.Sprintf("clovergo-%s-draft-%d.zip", draft.Platform, draft.ID), nil
}

// RenderDraftMarkdown lays a draft out the way a creator copies it into the
// WeChat editor: title, summary, lead, chapters and conclusion first, then the
// notes that are not published. The body is plain text, so what is written
// under each heading is exactly what gets pasted.
func RenderDraftMarkdown(draft domain.MediaDraft) string {
	var b strings.Builder
	platformName := draft.Platform
	if platform, err := platforms.Default(draft.Platform); err == nil {
		platformName = platform.Name
	}
	fmt.Fprintf(&b, "<!-- %s 草稿 · 主题：%s · 需人工发布 -->\n\n", platformName, strings.ReplaceAll(draft.Topic, "--", "- -"))
	gallery := draft.Gallery
	if draft.Format == platforms.FormatGallery || gallery != nil {
		if gallery == nil {
			return b.String()
		}
		fmt.Fprintf(&b, "# %s\n\n%s\n\n", gallery.Title, gallery.Caption)
		b.WriteString("---\n\n## 制作说明（不发布）\n\n")
		b.WriteString("### 图片 Prompt\n\n")
		for i, image := range gallery.Images {
			fmt.Fprintf(&b, "- 第 %d 张：%s\n", i+1, image.Prompt)
		}
		b.WriteString("\n> 推送到公众号草稿箱时，未上传的图片位由本机生成只含标题的卡片图（不使用上面的 Prompt）；发布前请人工检查。\n")
		return b.String()
	}
	article := draft.Article
	if article == nil {
		return b.String()
	}
	fmt.Fprintf(&b, "# %s\n\n%s\n\n", article.Title, article.Summary)
	fmt.Fprintf(&b, "%s\n\n", article.Lead)
	for _, section := range article.Sections {
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", section.Heading, section.Body)
	}
	fmt.Fprintf(&b, "## 结尾\n\n%s\n\n", article.Conclusion)
	b.WriteString("---\n\n## 制作说明（不发布）\n\n")
	fmt.Fprintf(&b, "- 摘要：%s\n- 封面配图 Prompt：%s\n- 标签：%s\n",
		article.Summary, article.CoverImagePrompt, strings.Join(article.Tags, "、"))
	withImage := make([]domain.ArticleSection, 0, len(article.Sections))
	for _, section := range article.Sections {
		if section.ImagePrompt != "" {
			withImage = append(withImage, section)
		}
	}
	if len(withImage) > 0 {
		b.WriteString("\n### 各章配图 Prompt 与图注\n\n")
		for _, section := range withImage {
			fmt.Fprintf(&b, "- %s：%s（图注：%s）\n", section.Heading, section.ImagePrompt, section.ImageCaption)
		}
	}
	b.WriteString("\n> 推送到公众号草稿箱时，封面与配图由本机生成只含标题的卡片图（不使用上面的 Prompt；没有本机转换工具时配图会被省略）；发布前请人工检查。\n")
	return b.String()
}
