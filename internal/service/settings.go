package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"clovergo/internal/platforms"
	"clovergo/internal/prompts"
	"clovergo/internal/repository"
)

// platform returns a platform with the owner's settings applied.
func (s *Service) platform(ctx context.Context, platformID string) (platforms.Platform, error) {
	platform, err := platforms.Default(platformID)
	if err != nil {
		return platforms.Platform{}, appError(CodeInvalidInput, "platform must be one of "+strings.Join(platforms.IDs(), ", "), err)
	}
	settings, revision, found, err := s.repo.PlatformSettings(ctx, platform.ID)
	if err != nil {
		return platforms.Platform{}, appError(CodeInternal, "read platform style failed", err)
	}
	if found {
		platform = platforms.Apply(platform, settings, revision)
	}
	return platform, nil
}

func (s *Service) ListPlatforms(ctx context.Context) ([]platforms.Platform, error) {
	ids := platforms.IDs()
	result := make([]platforms.Platform, 0, len(ids))
	for _, id := range ids {
		platform, err := s.platform(ctx, id)
		if err != nil {
			return nil, err
		}
		result = append(result, platform)
	}
	return result, nil
}

type PlatformStyleInput struct {
	platforms.Settings
	IfRevision int    `json:"if_revision"`
	Note       string `json:"note"`
}

// UpdatePlatformStyle saves owner settings as the platform's next revision.
func (s *Service) UpdatePlatformStyle(ctx context.Context, platformID string, input PlatformStyleInput) (platforms.Platform, error) {
	platform, err := s.platform(ctx, platformID)
	if err != nil {
		return platforms.Platform{}, err
	}
	settings, err := platforms.Validate(platform, input.Settings)
	if err != nil {
		return platforms.Platform{}, appError(CodeInvalidInput, err.Error(), err)
	}
	return s.savePlatformSettings(ctx, platform, input.IfRevision, settings, input.Note)
}

// ResetPlatformStyle records the built-in settings as a new revision.
func (s *Service) ResetPlatformStyle(ctx context.Context, platformID string, ifRevision int) (platforms.Platform, error) {
	platform, err := s.platform(ctx, platformID)
	if err != nil {
		return platforms.Platform{}, err
	}
	return s.savePlatformSettings(ctx, platform, ifRevision, platform.Defaults, "reset to default")
}

func (s *Service) savePlatformSettings(ctx context.Context, platform platforms.Platform, ifRevision int, settings platforms.Settings, note string) (platforms.Platform, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > 200 {
		return platforms.Platform{}, appError(CodeInvalidInput, "note must not exceed 200 characters", nil)
	}
	revision, err := s.repo.SavePlatformSettings(ctx, platform.ID, ifRevision, settings, note)
	if errors.Is(err, repository.ErrRevisionConflict) {
		return platforms.Platform{}, appError(CodeRevisionConflict, "the platform style was changed elsewhere; reload and try again", err)
	}
	if err != nil {
		return platforms.Platform{}, appError(CodeInternal, "save platform style failed", err)
	}
	s.log.InfoContext(ctx, "platform style revised", "platform", platform.ID, "revision", revision)
	return platforms.Apply(platform, settings, revision), nil
}

// PromptLayer is one editable prompt layer: the built-in text compiled from
// internal/prompts/layers, the text in effect, and the owner's revision when
// it has been replaced.
type PromptLayer struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	DefaultText string `json:"default_text"`
	Text        string `json:"text"`
	Revision    int    `json:"revision"`
	Customized  bool   `json:"customized"`
	UpdatedAt   string `json:"updated_at"`
}

// maxPromptLayerRunes caps one replacement. A layer longer than this is not a
// prompt any more; it is a document, and it would crowd out the material.
const maxPromptLayerRunes = 12000

// promptOverrides loads the owner's replacements for prompt composition.
func (s *Service) promptOverrides(ctx context.Context) (prompts.Overrides, error) {
	rows, err := s.repo.PromptOverrides(ctx)
	if err != nil {
		return nil, appError(CodeInternal, "read prompt overrides failed", err)
	}
	overrides := make(prompts.Overrides, len(rows))
	for key, row := range rows {
		if !prompts.IsLayer(key) {
			continue
		}
		overrides[key] = prompts.Override{Text: row.Text, Revision: row.Revision}
	}
	return overrides, nil
}

// ListPromptLayers returns every editable layer in composition order, with the
// built-in text alongside whatever is in effect.
func (s *Service) ListPromptLayers(ctx context.Context) ([]PromptLayer, error) {
	rows, err := s.repo.PromptOverrides(ctx)
	if err != nil {
		return nil, appError(CodeInternal, "read prompt overrides failed", err)
	}
	defs := prompts.Layers()
	result := make([]PromptLayer, 0, len(defs))
	for _, def := range defs {
		layer := PromptLayer{Key: def.Key, Name: def.Name, DefaultText: def.Text, Text: def.Text}
		if row, ok := rows[def.Key]; ok {
			layer.Text = row.Text
			layer.Revision = row.Revision
			layer.Customized = true
			layer.UpdatedAt = row.UpdatedAt
		}
		result = append(result, layer)
	}
	return result, nil
}

func (s *Service) promptLayer(ctx context.Context, key string) (*PromptLayer, error) {
	layers, err := s.ListPromptLayers(ctx)
	if err != nil {
		return nil, err
	}
	for _, layer := range layers {
		if layer.Key == key {
			return &layer, nil
		}
	}
	return nil, appError(CodeInvalidInput, "unknown prompt layer: "+key, nil)
}

type PromptLayerInput struct {
	Text       string `json:"text"`
	IfRevision int    `json:"if_revision"`
}

// SavePromptLayer replaces one layer's text as its next revision. The code
// validation in internal/ark and internal/service still applies: a prompt can
// ask for anything, and output that breaks the rules is still rejected.
func (s *Service) SavePromptLayer(ctx context.Context, key string, input PromptLayerInput) (*PromptLayer, error) {
	if !prompts.IsLayer(key) {
		return nil, appError(CodeInvalidInput, "unknown prompt layer: "+key, nil)
	}
	text := strings.TrimSpace(input.Text)
	if text == "" {
		return nil, appError(CodeInvalidInput, "the prompt layer must not be empty", nil)
	}
	if utf8.RuneCountInString(text) > maxPromptLayerRunes {
		return nil, appError(CodeInvalidInput, fmt.Sprintf("the prompt layer must not exceed %d characters", maxPromptLayerRunes), nil)
	}
	if _, err := s.repo.SavePromptOverride(ctx, key, text, input.IfRevision); err != nil {
		if errors.Is(err, repository.ErrRevisionConflict) {
			return nil, appError(CodeRevisionConflict, "the prompt layer was changed elsewhere; reload and try again", err)
		}
		return nil, appError(CodeInternal, "save prompt layer failed", err)
	}
	s.log.InfoContext(ctx, "prompt layer revised", "layer", key)
	return s.promptLayer(ctx, key)
}

// ResetPromptLayer removes the owner's replacement, restoring the built-in text.
func (s *Service) ResetPromptLayer(ctx context.Context, key string, ifRevision int) (*PromptLayer, error) {
	if !prompts.IsLayer(key) {
		return nil, appError(CodeInvalidInput, "unknown prompt layer: "+key, nil)
	}
	if err := s.repo.ResetPromptOverride(ctx, key, ifRevision); err != nil {
		if errors.Is(err, repository.ErrRevisionConflict) {
			return nil, appError(CodeRevisionConflict, "the prompt layer was changed elsewhere; reload and try again", err)
		}
		return nil, appError(CodeInternal, "reset prompt layer failed", err)
	}
	s.log.InfoContext(ctx, "prompt layer reset to built-in text", "layer", key)
	return s.promptLayer(ctx, key)
}

// ListPrompts returns every system prompt the library uses, layer by layer,
// exactly as it is sent to Ark. The owner may replace the Markdown layers;
// platform style layers are changed on their own screen, and the taxonomy and
// numeric format rules are generated from code.
func (s *Service) ListPrompts(ctx context.Context) ([]prompts.Composed, error) {
	overrides, err := s.promptOverrides(ctx)
	if err != nil {
		return nil, err
	}
	all, err := s.ListPlatforms(ctx)
	if err != nil {
		return nil, err
	}
	result := []prompts.Composed{
		prompts.Summary(overrides), prompts.Extraction(overrides), prompts.Answer(overrides), prompts.Agent(overrides),
	}
	for _, platform := range all {
		result = append(result, prompts.Draft(platform, overrides), prompts.Gallery(platform, overrides))
	}
	return result, nil
}
