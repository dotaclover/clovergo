package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"clovergo/internal/ark"
	"clovergo/internal/domain"
	"clovergo/internal/platforms"
	"clovergo/internal/prompts"
	"clovergo/internal/repository"
	"clovergo/internal/workflows"
)

const (
	runPurposeDraft = "media_draft"
	artifactSchema  = "1.0.0"
	// A draft is written from exactly one knowledge item. One article can only
	// carry one point, and grounding several unrelated items in one piece was
	// the fastest way to get a vague draft out of the model.
	maxDraftSourceKnowledge = 1
	// A draft can also be written straight from one material's original text:
	// a material is not necessarily knowledge, it may just be an archive.
	maxDraftSourceDocuments = 1
	// maxDraftDocumentRunes bounds how much original text one draft carries.
	// The whole material is used up to this limit, so a huge archive cannot
	// turn a single draft into a whole-context request.
	maxDraftDocumentRunes = 30000
	// defaultAudience replaces the old per-pack default: the library has one
	// audience profile now, and it is Chinese-language.
	defaultAudience = "对相关知识有兴趣的中文读者"
)

type GenerateDraftInput struct {
	Platform string `json:"platform"`
	// Format picks the shape: article (默认) or gallery (图文帖).
	Format   string `json:"format"`
	Topic    string `json:"topic"`
	Angle    string `json:"angle"`
	Audience string `json:"audience"`
	// KnowledgeIDs holds at most one ID: a named knowledge item. Empty means no
	// named item; RandomKnowledge is what asks for a picked one.
	KnowledgeIDs []int64 `json:"knowledge_ids"`
	// DocumentIDs names at most one material whose original text is the
	// source. It is what the materials list's "创作" action sends.
	DocumentIDs []int64 `json:"document_ids"`
	// RandomKnowledge asks for one knowledge item picked by the app, which is
	// the third source the screen offers next to a named item and a material.
	RandomKnowledge bool `json:"random_knowledge"`
	// ImageCount is how many pictures a gallery post gets; 0 means the default.
	ImageCount int `json:"image_count"`
}

// GenerateDraft accepts one article request and returns as soon as the draft row
// exists. The model call runs in the background queue, so the screen never waits
// on it; the list shows the draft as generating until the worker finishes.
func (s *Service) GenerateDraft(ctx context.Context, input GenerateDraftInput) (*domain.MediaDraft, error) {
	platform, err := s.platform(ctx, input.Platform)
	if err != nil {
		return nil, err
	}
	input.Platform = platform.ID
	input.Format = strings.TrimSpace(input.Format)
	if input.Format == "" {
		input.Format = platform.Format
	}
	if !platforms.SupportsFormat(input.Format) {
		return nil, appError(CodeInvalidInput, "format must be article or gallery", nil)
	}
	input.Topic = strings.TrimSpace(input.Topic)
	if input.Format == platforms.FormatGallery {
		if input.ImageCount == 0 {
			input.ImageCount = platforms.GalleryImageDefaultCount
		}
		if input.ImageCount < platforms.GalleryImageMinCount || input.ImageCount > platforms.GalleryImageMaxCount {
			return nil, appError(CodeInvalidInput, fmt.Sprintf("image_count must be %d-%d", platforms.GalleryImageMinCount, platforms.GalleryImageMaxCount), nil)
		}
	} else {
		input.ImageCount = 0
	}
	input.Angle = strings.TrimSpace(input.Angle)
	input.Audience = strings.TrimSpace(input.Audience)
	if input.Topic == "" && input.Angle == "" && input.Audience == "" &&
		len(input.KnowledgeIDs) == 0 && len(input.DocumentIDs) == 0 && !input.RandomKnowledge {
		return nil, appError(CodeInvalidInput, "an inspiration (topic, angle or audience) or a source is required", nil)
	}
	if utf8.RuneCountInString(input.Topic) > 500 {
		return nil, appError(CodeInvalidInput, "the inspiration must not exceed 500 characters", nil)
	}
	if utf8.RuneCountInString(input.Angle) > 500 || utf8.RuneCountInString(input.Audience) > 200 {
		return nil, appError(CodeInvalidInput, "angle or audience is too long", nil)
	}
	if input.Audience == "" {
		input.Audience = defaultAudience
	}
	if input.KnowledgeIDs, err = selectedIDs(input.KnowledgeIDs, maxDraftSourceKnowledge, "knowledge_ids"); err != nil {
		return nil, err
	}
	if input.DocumentIDs, err = selectedIDs(input.DocumentIDs, maxDraftSourceDocuments, "document_ids"); err != nil {
		return nil, err
	}
	if configErr := s.ai.ConfigurationError(); configErr != nil {
		return nil, appError(CodeArkNotConfigured, configErr.Error(), configErr)
	}

	selectionStarted := time.Now()
	documents, err := s.draftDocuments(ctx, input.DocumentIDs)
	if err != nil {
		return nil, mapSelectionError(err, "select draft material failed")
	}
	var knowledge []domain.KnowledgeItem
	switch {
	case len(input.KnowledgeIDs) > 0:
		knowledge, err = s.selectedKnowledge(ctx, input.KnowledgeIDs)
		if err != nil {
			return nil, mapSelectionError(err, "select draft knowledge failed")
		}
	case len(documents) == 0 && input.RandomKnowledge:
		knowledge, err = s.draftKnowledge(ctx, nil)
		if err != nil {
			return nil, mapSelectionError(err, "select draft knowledge failed")
		}
	}
	// The inspiration is optional: when only a source was handed over, the
	// source names the draft. A material's filename is used without its
	// extension so the topic reads like a subject, not a file.
	if input.Topic == "" {
		switch {
		case len(knowledge) > 0:
			input.Topic = capRunes(knowledge[0].Title, 200)
		case len(documents) > 0:
			input.Topic = capRunes(strings.TrimSuffix(documents[0].DocumentName, filepath.Ext(documents[0].DocumentName)), 200)
		case input.Angle != "":
			input.Topic = capRunes(input.Angle, 200)
		default:
			input.Topic = capRunes(input.Audience, 200)
		}
	}
	if input.Topic == "" {
		return nil, appError(CodeInvalidInput, "an inspiration (topic, angle or audience) or a source is required", nil)
	}

	overrides, err := s.promptOverrides(ctx)
	if err != nil {
		return nil, err
	}
	system := prompts.Draft(platform, overrides)
	if input.Format == platforms.FormatGallery {
		system = prompts.Gallery(platform, overrides)
	}
	runID, runStarted, err := s.startWorkflowRun(ctx, workflows.MediaDraftKey, runPurposeDraft, input)
	if err != nil {
		return nil, err
	}
	fail := func(draftID int64, cause error) (*domain.MediaDraft, error) {
		if draftID > 0 {
			if failErr := s.repo.FailMediaDraft(ctx, draftID, cause.Error()); failErr != nil {
				s.log.ErrorContext(ctx, "mark media draft failed", "draft_id", draftID, "error", failErr)
			}
		}
		_ = s.finishRun(ctx, runID, runPurposeDraft, runStarted, domain.TokenUsage{}, cause)
		return nil, withRunID(cause, runID)
	}

	draft, err := s.repo.CreatePendingMediaDraft(ctx, domain.MediaDraft{
		RunID: runID, Platform: platform.ID, Format: input.Format,
		Topic: input.Topic, Angle: input.Angle, Audience: input.Audience,
		Model: s.ai.Model(), PromptVersion: system.PromptVersion(),
	}, domain.Artifact{
		RunID: runID, Ordinal: 1, SchemaVersion: artifactSchema, Title: "generating",
		Model: s.ai.Model(), PromptVersion: system.PromptVersion(),
	})
	if err != nil {
		return fail(0, appError(CodeInternal, "save media draft failed", err))
	}
	if err := s.addAgentEvent(ctx, runID, 1, "select_knowledge", domain.RunSucceeded, selectionStarted, nil, map[string]any{
		"platform": platform.ID, "knowledge_ids": knowledgeItemIDs(knowledge), "document_ids": documentIDs(documents),
	}); err != nil {
		if failErr := s.repo.FailMediaDraft(ctx, draft.ID, err.Error()); failErr != nil {
			s.log.ErrorContext(ctx, "mark media draft failed", "draft_id", draft.ID, "error", failErr)
		}
		_ = s.finishRun(ctx, runID, runPurposeDraft, runStarted, domain.TokenUsage{}, err)
		return nil, withRunID(err, runID)
	}
	if !s.drafts.Enqueue(draftJob{
		draftID: draft.ID, runID: runID, runStarted: runStarted, input: input,
		platform: platform, system: system,
		knowledge: knowledge, documents: documents,
	}) {
		return fail(draft.ID, appError(CodeInternal, "the draft queue is not accepting work", nil))
	}
	s.log.InfoContext(ctx, "media draft queued", "draft_id", draft.ID, "platform", platform.ID, "format", input.Format, "run_id", runID,
		"knowledge_count", len(knowledge), "document_count", len(documents))
	return draft, nil
}

// processDraftJob writes one queued draft. It owns the rest of the Run: the
// request returned long ago, so the outcome lives on the draft row. ctx is
// cancelled at shutdown; the bookkeeping after the model call uses a context
// that outlives that cancellation so the draft row still records the outcome.
func (s *Service) processDraftJob(ctx context.Context, job draftJob) {
	store := context.WithoutCancel(ctx)
	usage := domain.TokenUsage{}
	defer func() {
		if r := recover(); r != nil {
			s.log.ErrorContext(store, "media draft panicked", "draft_id", job.draftID, "run_id", job.runID, "panic", fmt.Sprint(r))
			if failErr := s.repo.FailMediaDraft(store, job.draftID, "draft generation crashed unexpectedly"); failErr != nil {
				s.log.ErrorContext(store, "mark media draft failed", "draft_id", job.draftID, "error", failErr)
			}
			_ = s.finishRun(store, job.runID, runPurposeDraft, job.runStarted, usage, appError(CodeInternal, "draft generation crashed unexpectedly", nil))
		}
	}()
	draft, err := s.writeDraft(ctx, &job, &usage)
	if err == nil {
		// The draft row is already complete: the usage counters and the Run
		// summary are bookkeeping, and a failure to close the Run is logged
		// rather than turning a stored article back into a failed one.
		s.recordDraftUsage(store, &job)
		if runErr := s.finishRun(store, job.runID, runPurposeDraft, job.runStarted, usage, nil); runErr != nil {
			s.log.ErrorContext(store, "finish media draft run failed", "draft_id", job.draftID, "run_id", job.runID, "error", runErr.Error())
		}
		s.log.InfoContext(store, "media draft generated", "draft_id", draft.ID, "platform", job.platform.ID, "run_id", job.runID)
		return
	}
	message := err.Error()
	var appErr *AppError
	if errors.As(err, &appErr) {
		message = appErr.Message
	}
	if failErr := s.repo.FailMediaDraft(store, job.draftID, message); failErr != nil {
		s.log.ErrorContext(store, "mark media draft failed", "draft_id", job.draftID, "error", failErr)
	}
	_ = s.finishRun(store, job.runID, runPurposeDraft, job.runStarted, usage, err)
	s.log.ErrorContext(store, "media draft failed", "draft_id", job.draftID, "run_id", job.runID, "error", message)
}

// recordDraftUsage bumps the counters of the knowledge item and the material
// this draft was written from. Drafts no longer record their sources, so these
// counters are the only trace of what has been used; a failure here is logged
// and changes nothing else, because the draft is already stored.
func (s *Service) recordDraftUsage(ctx context.Context, job *draftJob) {
	if err := s.repo.IncrementKnowledgeUse(ctx, knowledgeItemIDs(job.knowledge)); err != nil {
		s.log.ErrorContext(ctx, "record draft knowledge usage failed", "draft_id", job.draftID, "error", err.Error())
	}
	if err := s.repo.IncrementDocumentUse(ctx, documentIDs(job.documents)); err != nil {
		s.log.ErrorContext(ctx, "record draft document usage failed", "draft_id", job.draftID, "error", err.Error())
	}
}

// maxDraftAttempts is how many times one draft is asked for. The model
// occasionally returns malformed tool arguments or a truncated response; one
// retry turns that into a slow success instead of a failed draft.
const maxDraftAttempts = 2

// writeDraft calls the model and stores the article over the pending row. An
// invalid response is retried once; every attempt is its own Run event, so the
// trace shows exactly what happened.
func (s *Service) writeDraft(ctx context.Context, job *draftJob, usage *domain.TokenUsage) (*domain.MediaDraft, error) {
	ordinal := 2
	for attempt := 1; attempt <= maxDraftAttempts; attempt++ {
		generationStarted := time.Now()
		body, generationUsage, err := s.ai.DraftForPlatform(ctx, job.platform, job.system.Text, domain.MediaDraftInput{
			Format: job.input.Format, Topic: job.input.Topic, Angle: job.input.Angle, Audience: job.input.Audience, ImageCount: job.input.ImageCount,
		}, job.knowledge, job.documents)
		usage.Add(generationUsage)
		if err != nil {
			mapped := mapArkError(err, "Ark draft request failed", "Ark returned an invalid draft")
			if traceErr := s.addAgentEvent(ctx, job.runID, ordinal, "draft_for_platform", domain.RunFailed, generationStarted, mapped, map[string]any{
				"platform": job.platform.ID, "model": s.ai.Model(), "attempt": attempt,
			}); traceErr != nil {
				return nil, traceErr
			}
			ordinal++
			if errors.Is(err, ark.ErrInvalidResponse) && attempt < maxDraftAttempts {
				s.log.WarnContext(ctx, "media draft response was invalid, retrying", "draft_id", job.draftID,
					"run_id", job.runID, "attempt", attempt, "error", mapped.Error())
				continue
			}
			return nil, mapped
		}
		trace := map[string]any{"platform": job.platform.ID, "format": job.input.Format, "model": s.ai.Model(),
			"prompt_version": job.system.PromptVersion(), "prompt_sha256": job.system.SHA256, "attempt": attempt}
		if err := s.addAgentEvent(ctx, job.runID, ordinal, "draft_for_platform", domain.RunSucceeded, generationStarted, nil, trace); err != nil {
			return nil, err
		}
		ordinal++
		draft, err := s.repo.CompleteMediaDraft(ctx, job.draftID, domain.MediaDraft{
			RunID: job.runID, Platform: job.platform.ID, Format: job.input.Format,
			Topic: job.input.Topic, Angle: job.input.Angle, Audience: job.input.Audience,
			Article: body.Article, Gallery: body.Gallery,
			Model: s.ai.Model(), PromptVersion: job.system.PromptVersion(),
		}, draftTitle(body))
		if err != nil {
			return nil, appError(CodeInternal, "save media draft failed", err)
		}
		// The Markdown copy is a convenience next to the database, never the
		// record itself: a failed write leaves the draft intact and is only
		// logged.
		markdownPath, writeErr := s.writeDraftMarkdown(draft)
		if writeErr != nil {
			s.log.ErrorContext(ctx, "save draft markdown failed", "draft_id", draft.ID, "error", writeErr.Error())
			markdownPath = ""
		}
		saveTrace := map[string]any{"media_draft_id": draft.ID, "artifact_id": draft.ArtifactID}
		if markdownPath != "" {
			saveTrace["markdown_path"] = markdownPath
		}
		if err := s.addAgentEvent(ctx, job.runID, ordinal, "save_artifact", domain.RunSucceeded, time.Now(), nil, saveTrace); err != nil {
			return nil, err
		}
		return draft, nil
	}
	return nil, appError(CodeInternal, "the draft was not written", nil)
}

func (s *Service) ListDrafts(ctx context.Context, platform string, limit, offset int) ([]domain.MediaDraft, int, error) {
	if platform = strings.TrimSpace(platform); platform != "" {
		resolved, err := platforms.Default(platform)
		if err != nil {
			return nil, 0, appError(CodeInvalidInput, "platform is unknown", err)
		}
		platform = resolved.ID
	}
	drafts, err := s.repo.ListMediaDrafts(ctx, platform, normalizeLimit(limit, 20), normalizeOffset(offset))
	if err != nil {
		return nil, 0, appError(CodeInternal, "list media drafts failed", err)
	}
	total, err := s.repo.CountMediaDrafts(ctx, platform)
	if err != nil {
		return nil, 0, appError(CodeInternal, "count media drafts failed", err)
	}
	return drafts, total, nil
}

func (s *Service) GetDraft(ctx context.Context, id int64) (*domain.MediaDraft, error) {
	if id <= 0 {
		return nil, appError(CodeInvalidInput, "draft id must be positive", nil)
	}
	draft, err := s.repo.GetMediaDraft(ctx, id)
	if err != nil {
		return nil, mapRepositoryError(err, CodeMediaDraftNotFound)
	}
	return draft, nil
}

// UpdateDraft replaces a generated draft's content with an owner edit. The edit
// goes through the same validation as model output, so a hand-written change
// cannot save a draft in a shape the generator would refuse.
func (s *Service) UpdateDraft(ctx context.Context, id int64, body domain.MediaDraftBody) (*domain.MediaDraft, error) {
	if id <= 0 {
		return nil, appError(CodeInvalidInput, "draft id must be positive", nil)
	}
	current, err := s.GetDraft(ctx, id)
	if err != nil {
		return nil, err
	}
	var title string
	switch current.Format {
	case platforms.FormatGallery:
		if body.Gallery == nil {
			return nil, appError(CodeInvalidInput, "a complete picture post is required", nil)
		}
		if err := ark.ValidateGallery(body.Gallery); err != nil {
			return nil, appError(CodeInvalidInput, err.Error(), err)
		}
		body.Article = nil
		title = body.Gallery.Title
	case platforms.FormatArticle:
		if body.Article == nil {
			return nil, appError(CodeInvalidInput, "a complete article is required", nil)
		}
		if err := ark.ValidateArticle(body.Article); err != nil {
			return nil, appError(CodeInvalidInput, err.Error(), err)
		}
		body.Gallery = nil
		title = body.Article.Title
	default:
		return nil, appError(CodeInvalidInput, "the draft has an unknown format", nil)
	}
	draft, err := s.repo.UpdateMediaDraftBody(ctx, id, body, title)
	if err != nil {
		return nil, mapRepositoryError(err, CodeMediaDraftNotFound)
	}
	if _, err := s.writeDraftMarkdown(draft); err != nil {
		s.log.ErrorContext(ctx, "save draft markdown failed", "draft_id", draft.ID, "error", err.Error())
	}
	s.log.InfoContext(ctx, "media draft edited", "draft_id", draft.ID)
	return draft, nil
}

// DeleteDraft removes a draft, its artifact and its Markdown copy.
func (s *Service) DeleteDraft(ctx context.Context, id int64) error {
	if id <= 0 {
		return appError(CodeInvalidInput, "draft id must be positive", nil)
	}
	if err := s.repo.DeleteMediaDraft(ctx, id); err != nil {
		return mapRepositoryError(err, CodeMediaDraftNotFound)
	}
	s.removeDraftMarkdown(id)
	s.log.InfoContext(ctx, "media draft deleted", "draft_id", id)
	return nil
}

// startWorkflowRun records the workflow and input of a generation run before
// any model call so even immediate failures stay traceable. Knowledge spaces
// no longer scope a run: every generation draws on the one library.
func (s *Service) startWorkflowRun(ctx context.Context, workflowKey, purpose string, input any) (string, time.Time, error) {
	workflow, err := workflows.GetLatest(workflowKey)
	if err != nil {
		return "", time.Time{}, appError(CodeInternal, "workflow is unavailable", err)
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return "", time.Time{}, appError(CodeInternal, "encode workflow input failed", err)
	}
	inputHash := sha256.Sum256(inputJSON)
	runID, err := newRunID()
	if err != nil {
		return "", time.Time{}, appError(CodeInternal, "generate run id failed", err)
	}
	started := time.Now()
	if err := s.repo.CreateWorkflowRun(ctx, domain.WorkflowRun{
		RunID: runID, WorkflowKey: workflow.Key, WorkflowVersion: workflow.Version,
		InputJSON: inputJSON, InputSHA256: hex.EncodeToString(inputHash[:]),
	}, domain.AgentRun{RunID: runID, Purpose: purpose, Status: domain.RunRunning}); err != nil {
		return "", time.Time{}, appError(CodeInternal, "create workflow run failed", err)
	}
	return runID, started, nil
}

// draftKnowledge returns the single item a draft is written from: the one the
// creator picked, or a random item no draft has used yet.
//
// The random branch replaces the old retrieval-driven selection. Drafts used
// to be handed several items retrieved by topic+angle, which produced pieces
// that touched many things and said none of them well. Picking one unused item
// at random keeps the library moving and keeps each draft narrow.
func (s *Service) draftKnowledge(ctx context.Context, ids []int64) ([]domain.KnowledgeItem, error) {
	if len(ids) > 0 {
		return s.selectedKnowledge(ctx, ids)
	}
	id, err := s.repo.RandomKnowledgeID(ctx, repository.KnowledgeFilter{Usage: repository.UsageUnused})
	if errors.Is(err, repository.ErrNotFound) {
		// Every item has been written about at least once. Start over from a
		// random one rather than refusing to generate.
		id, err = s.repo.RandomKnowledgeID(ctx, repository.KnowledgeFilter{Usage: repository.UsageAll})
	}
	if err != nil {
		return nil, err
	}
	items, err := s.repo.GetKnowledgeByIDs(ctx, []int64{id})
	if err != nil {
		return nil, err
	}
	current, _ := currentKnowledge(items)
	return current, nil
}

// selectedKnowledge loads explicitly chosen knowledge and refuses items that
// only cite superseded document versions.
func (s *Service) selectedKnowledge(ctx context.Context, ids []int64) ([]domain.KnowledgeItem, error) {
	knowledge, err := s.repo.GetKnowledgeByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	current, dropped := currentKnowledge(knowledge)
	if dropped > 0 {
		return nil, appError(CodeNoGroundedKnowledge, "one or more selected knowledge IDs only cite a superseded document version", nil)
	}
	return current, nil
}

func mapSelectionError(err error, message string) error {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return err
	}
	if errors.Is(err, repository.ErrNotFound) {
		return appError(CodeNoGroundedKnowledge, "one or more selected knowledge IDs do not exist", err)
	}
	return appError(CodeInternal, message, err)
}

func selectedIDs(ids []int64, max int, field string) ([]int64, error) {
	for _, id := range ids {
		if id <= 0 {
			return nil, appError(CodeInvalidInput, field+" must contain only positive integers", nil)
		}
	}
	ids = uniquePositiveIDs(ids)
	if len(ids) > max {
		return nil, appError(CodeInvalidInput, fmt.Sprintf("%s must contain at most %d IDs", field, max), nil)
	}
	return ids, nil
}

// draftDocuments loads the original text of the chosen materials. A material
// is used whole; only a very large one is cut, and the cut is stated in the
// text itself so the model does not treat a truncated ending as the real one.
func (s *Service) draftDocuments(ctx context.Context, ids []int64) ([]domain.SourceDocument, error) {
	documents := make([]domain.SourceDocument, 0, len(ids))
	for _, id := range ids {
		source, err := s.GetSourceDocument(ctx, id)
		if err != nil {
			return nil, err
		}
		source.Text = capDocumentText(source.Text)
		documents = append(documents, source)
	}
	return documents, nil
}

func capDocumentText(text string) string {
	if utf8.RuneCountInString(text) <= maxDraftDocumentRunes {
		return text
	}
	return capRunes(text, maxDraftDocumentRunes) + "\n\n（原文过长，以上为前 30000 字，以下略。）"
}

func capRunes(text string, max int) string {
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	return string([]rune(text)[:max])
}

func documentIDs(documents []domain.SourceDocument) []int64 {
	ids := make([]int64, 0, len(documents))
	for _, document := range documents {
		ids = append(ids, document.DocumentID)
	}
	return ids
}

func uniquePositiveIDs(ids []int64) []int64 {
	result := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func knowledgeItemIDs(items []domain.KnowledgeItem) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func draftTitle(body domain.MediaDraftBody) string {
	if body.Article != nil {
		return body.Article.Title
	}
	if body.Gallery != nil {
		return body.Gallery.Title
	}
	return ""
}

// writeDraftMarkdown saves the finished article as data/drafts/<id>-<title>.md,
// next to the material library. The file is the same Markdown the export ZIP
// carries, so the creator can open and copy it without going through the UI.
func (s *Service) writeDraftMarkdown(draft *domain.MediaDraft) (string, error) {
	dir := filepath.Join(filepath.Dir(s.libraryDir), "drafts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	title := ""
	if draft.Article != nil {
		title = draft.Article.Title
	} else if draft.Gallery != nil {
		title = draft.Gallery.Title
	}
	// An edit can change the title, so the previous copy is removed first: one
	// draft must not leave two Markdown files behind.
	s.removeDraftMarkdown(draft.ID)
	path := filepath.Join(dir, fmt.Sprintf("%d-%s.md", draft.ID, fileSegment(title)))
	if err := os.WriteFile(path, []byte(RenderDraftMarkdown(*draft)), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// removeDraftMarkdown deletes any Markdown copy of one draft.
func (s *Service) removeDraftMarkdown(id int64) {
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(s.libraryDir), "drafts", fmt.Sprintf("%d-*.md", id)))
	if err != nil {
		return
	}
	for _, path := range matches {
		_ = os.Remove(path)
	}
}

// fileSegment turns an article title into a name Windows accepts: reserved
// characters and control characters become dashes, and the result is capped so
// the path stays short.
func fileSegment(title string) string {
	name := strings.NewReplacer(`\`, "-", "/", "-", ":", "-", "*", "-", "?", "-",
		`"`, "-", "<", "-", ">", "-", "|", "-").Replace(title)
	name = strings.Map(func(r rune) rune {
		if r < 0x20 {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(capRunes(strings.TrimSpace(name), 40))
	name = strings.Trim(name, " .")
	if name == "" {
		return "draft"
	}
	return name
}
