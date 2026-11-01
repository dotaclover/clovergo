package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"clovergo/internal/ark"
	"clovergo/internal/domain"
	"clovergo/internal/extractor"
	"clovergo/internal/prompts"
	"clovergo/internal/repository"
)

const runPurposeKnowledgeIngestion = "knowledge_ingestion"

type ImportResult struct {
	Document  *domain.Document `json:"document"`
	Duplicate bool             `json:"duplicate"`
}

// MaterialImportResult is what an import returns. The whole material was read
// once and summarized before it was stored; knowledge is extracted only when
// the owner asks for it, so an import never spends a second model call.
type MaterialImportResult struct {
	Document  *domain.Document `json:"document"`
	Duplicate bool             `json:"duplicate"`
}

// ImportMaterial stores an uploaded file and summarizes it. Importing is only
// archiving: the material sits in the library until the owner decides to
// extract knowledge from it (POST /documents/:id/reprocess), because a file is
// often kept as a record rather than as a source of claims.
func (s *Service) ImportMaterial(ctx context.Context, filename string, data []byte, topic string) (MaterialImportResult, error) {
	imported, err := s.storeMaterial(ctx, filename, data, topic, materialOrigin{})
	if err != nil {
		return MaterialImportResult{}, err
	}
	return MaterialImportResult{Document: imported.Document, Duplicate: imported.Duplicate}, nil
}

// ImportText stores pasted content. Only the text is supplied: the material is
// named after its first meaningful line, so nothing has to be filled in.
func (s *Service) ImportText(ctx context.Context, text, topic string) (MaterialImportResult, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return MaterialImportResult{}, appError(CodeDocumentEmpty, "text is empty", nil)
	}
	return s.ImportMaterial(ctx, derivedFilename(text), []byte(text), topic)
}

// UpdateDocumentTopic re-files one material. Topics are free text an operator
// chooses; an empty topic means "unfiled".
func (s *Service) UpdateDocumentTopic(ctx context.Context, documentID int64, topic string) (*domain.Document, error) {
	if documentID <= 0 {
		return nil, appError(CodeInvalidInput, "document id must be positive", nil)
	}
	document, err := s.repo.UpdateDocumentTopic(ctx, documentID, normalizeTopic(topic))
	if err != nil {
		return nil, mapRepositoryError(err, CodeDocumentNotFound)
	}
	s.log.InfoContext(ctx, "document topic updated", "document_id", documentID, "topic", document.Topic)
	return document, nil
}

// RenameDocument changes a material's display name.
func (s *Service) RenameDocument(ctx context.Context, documentID int64, filename string) (*domain.Document, error) {
	if documentID <= 0 {
		return nil, appError(CodeInvalidInput, "document id must be positive", nil)
	}
	filename = strings.Join(strings.Fields(filename), " ")
	if filename == "" || utf8.RuneCountInString(filename) > 200 {
		return nil, appError(CodeInvalidInput, "filename must be 1-200 characters", nil)
	}
	document, err := s.repo.RenameDocument(ctx, documentID, filename)
	if err != nil {
		return nil, mapRepositoryError(err, CodeDocumentNotFound)
	}
	s.log.InfoContext(ctx, "document renamed", "document_id", documentID)
	return document, nil
}

// DeleteDocument removes a material together with its knowledge and vectors,
// then the stored file. A material that is being processed is refused.
func (s *Service) DeleteDocument(ctx context.Context, documentID int64) error {
	if documentID <= 0 {
		return appError(CodeInvalidInput, "document id must be positive", nil)
	}
	path, shared, err := s.repo.DeleteDocument(ctx, documentID, domain.DocumentProcessing)
	if err != nil {
		return mapRepositoryError(err, CodeDocumentNotFound)
	}
	if !shared && path != "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.log.WarnContext(ctx, "delete stored file failed", "document_id", documentID, "error", err)
		}
	}
	s.log.InfoContext(ctx, "document deleted", "document_id", documentID)
	return nil
}

// normalizeTopic collapses whitespace in an operator-supplied topic so that
// " 日语 " and "日语" are one shelf rather than two. Topics are otherwise taken
// verbatim: they are a naming decision, not a value to validate.
func normalizeTopic(topic string) string {
	return strings.Join(strings.Fields(topic), " ")
}

// defaultTopic falls back to the detected language when no topic was given.
// The materials people import are usually language practice, so ja and en get
// a usable shelf name for free. Everything else stays unfiled rather than
// guessed at, because a wrong topic is worse than an empty one -- and F1 or
// 乒乓球 cannot be deduced from the text at all.
func defaultTopic(topic, language string) string {
	if topic = normalizeTopic(topic); topic != "" {
		return topic
	}
	switch language {
	case domain.LanguageJapanese:
		return "日语"
	case domain.LanguageEnglish:
		return "英语"
	default:
		return ""
	}
}

// ReprocessDocument queues a material again, for example after a failed
// extraction. Extraction runs on the whole material in one call, so there is no
// partial work to resume: the material is reprocessed from the start.
func (s *Service) ReprocessDocument(ctx context.Context, documentID int64) (MaterialImportResult, error) {
	if documentID <= 0 {
		return MaterialImportResult{}, appError(CodeInvalidInput, "document id must be positive", nil)
	}
	requeued, err := s.repo.RequeueDocument(ctx, documentID, domain.DocumentProcessing, domain.DocumentReady)
	if err != nil {
		return MaterialImportResult{}, mapRepositoryError(err, CodeDocumentNotFound)
	}
	if !requeued {
		return MaterialImportResult{}, appError(CodeInvalidState, "document is already being processed", nil)
	}
	s.queue.Enqueue(documentID)
	refreshed, err := s.repo.GetDocument(ctx, documentID)
	if err != nil {
		return MaterialImportResult{}, mapRepositoryError(err, CodeDocumentNotFound)
	}
	return MaterialImportResult{Document: refreshed}, nil
}

// derivedFilename names a pasted material after its first meaningful line.
func derivedFilename(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "#>*-+ \t"))
		if line == "" {
			continue
		}
		runes := []rune(line)
		if len(runes) > 60 {
			runes = runes[:60]
		}
		return sanitizeFilename(string(runes)) + ".md"
	}
	return "粘贴内容.md"
}

func sanitizeFilename(name string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`\/:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return "粘贴内容"
	}
	return cleaned
}

// storeMaterial validates the upload, reads it once, summarizes it, writes it
// to the library, and records the document in 'ready'. Identical content is
// reported as a duplicate instead of being imported twice.
func (s *Service) storeMaterial(ctx context.Context, filename string, data []byte, topic string, origin materialOrigin) (ImportResult, error) {
	if len(data) == 0 {
		return ImportResult{}, appError(CodeDocumentEmpty, "document is empty", nil)
	}
	if int64(len(data)) > s.maxUploadBytes {
		return ImportResult{}, appError(CodeFileTooLarge, "document exceeds configured upload limit", nil)
	}
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "." || filename == "" {
		return ImportResult{}, appError(CodeInvalidInput, "filename is required", nil)
	}

	prepared, err := prepareMaterial(filename, data, s.maxDocumentRunes)
	if err != nil {
		return ImportResult{}, err
	}
	mediaType, text, hash := prepared.MediaType, prepared.Text, prepared.Hash
	sourceKey := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(filename, filepath.Ext(filename))))
	if origin.RelPath != "" {
		// A file under a library root is identified by where it lives, so a
		// changed file becomes a new version of the same source.
		sourceKey = fmt.Sprintf("root%d/%s", origin.RootID, strings.ToLower(origin.RelPath))
	}
	if sourceKey == "" {
		sourceKey = hash
	}
	// A duplicate is the same content as the newest version of the same
	// source; the same bytes under another source are a separate material.
	existing, err := s.repo.FindLatestDocumentBySource(ctx, sourceKey)
	if err == nil && existing.SHA256 == hash {
		return ImportResult{Document: existing, Duplicate: true}, nil
	}
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return ImportResult{}, mapRepositoryError(err, CodeDocumentNotFound)
	}

	// The summary is the one synchronous model call at upload: it is what the
	// uploader sees while the background extraction runs. A missing key or an
	// unreachable Ark fails the import outright rather than storing a material
	// with no summary.
	if configErr := s.ai.ConfigurationError(); configErr != nil {
		return ImportResult{}, appError(CodeArkNotConfigured, configErr.Error(), configErr)
	}
	overrides, err := s.promptOverrides(ctx)
	if err != nil {
		return ImportResult{}, err
	}
	summaryStarted := time.Now()
	summary, usage, err := s.ai.Summarize(ctx, prompts.Summary(overrides).Text, text)
	if err != nil {
		return ImportResult{}, mapArkError(err, "Ark summary request failed", "Ark returned invalid summary data")
	}
	s.log.InfoContext(ctx, "material summarized", "bytes", len(data), "summary_runes", len([]rune(summary)),
		"duration_ms", time.Since(summaryStarted).Milliseconds(), "tokens_in", usage.InputTokens, "tokens_out", usage.OutputTokens)

	if err := os.MkdirAll(s.libraryDir, 0o755); err != nil {
		return ImportResult{}, appError(CodeInternal, "create library directory failed", err)
	}
	storedPath := filepath.Join(s.libraryDir, hash+prepared.Ext)
	_, statErr := os.Stat(storedPath)
	wroteFile := errors.Is(statErr, os.ErrNotExist)
	if err := os.WriteFile(storedPath, prepared.Stored, 0o644); err != nil {
		return ImportResult{}, appError(CodeInternal, "store document failed", err)
	}

	language := detectLanguage(sampleRunes(text, 4000))
	document, err := s.repo.CreateDocument(ctx, domain.Document{
		Filename: filename, SourceKey: sourceKey, SHA256: hash, MediaType: mediaType,
		Language: language, Topic: defaultTopic(topic, language), Summary: summary, StoredPath: storedPath,
		RootID: origin.RootID, RelPath: origin.RelPath, OriginSize: origin.Size, OriginMTime: origin.MTime,
	})
	if err != nil {
		if existing, findErr := s.repo.FindLatestDocumentBySource(ctx, sourceKey); findErr == nil && existing.SHA256 == hash {
			return ImportResult{Document: existing, Duplicate: true}, nil
		}
		if wroteFile {
			if removeErr := os.Remove(storedPath); removeErr != nil {
				s.log.WarnContext(ctx, "remove orphaned library file failed", "error", removeErr)
			}
		}
		return ImportResult{}, appError(CodeInternal, "save document failed", err)
	}
	s.log.InfoContext(ctx, "material imported", "document_id", document.ID, "language", document.Language,
		"topic", document.Topic, "runes", utf8.RuneCountInString(text))
	return ImportResult{Document: document}, nil
}

// materialOrigin says where a file came from when it was found under a library
// root; uploads and pasted text have none.
type materialOrigin struct {
	RootID  int64
	RelPath string
	Size    int64
	MTime   int64
}

// preparedMaterial is a file converted to what the library keeps. Converted
// formats (docx) are stored as their plain text, so the stored snapshot is
// exactly the text that Evidence offsets are computed against.
type preparedMaterial struct {
	MediaType string
	Text      string
	Stored    []byte
	Ext       string
	Hash      string
}

func prepareMaterial(filename string, data []byte, maxRunes int) (preparedMaterial, error) {
	mediaType, text, err := extractor.ExtractText(filename, data, maxRunes)
	if err != nil {
		return preparedMaterial{}, mapExtractError(err)
	}
	stored, ext := data, strings.ToLower(filepath.Ext(filename))
	if extractor.Converted(filename) {
		stored, ext = []byte(text), ".txt"
	}
	sum := sha256.Sum256(stored)
	return preparedMaterial{MediaType: mediaType, Text: text, Stored: stored, Ext: ext, Hash: hex.EncodeToString(sum[:])}, nil
}

func mapExtractError(err error) error {
	switch {
	case errors.Is(err, extractor.ErrUnsupportedFormat):
		return appError(CodeUnsupportedFormat, "only UTF-8 .md, .markdown, .txt and .docx files are supported", err)
	case errors.Is(err, extractor.ErrInvalidUTF8):
		return appError(CodeInvalidInput, "document must be UTF-8", err)
	case errors.Is(err, extractor.ErrInvalidDocx):
		return appError(CodeInvalidInput, "document is not a readable .docx file", err)
	case errors.Is(err, extractor.ErrEmptyDocument):
		return appError(CodeDocumentEmpty, "document contains no text", err)
	case errors.Is(err, extractor.ErrTooLong):
		return appError(CodeFileTooLarge, "document exceeds the configured rune limit", err)
	default:
		return appError(CodeInternal, "document extraction failed", err)
	}
}

// sampleRunes returns at most maxRunes leading runes, enough text to detect a
// language without walking a whole book.
func sampleRunes(text string, maxRunes int) string {
	if utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	count := 0
	for index := range text {
		if count == maxRunes {
			return text[:index]
		}
		count++
	}
	return text
}

// processQueued runs one extraction for the background queue. A failure is
// logged and recorded on the document; it never takes the worker down.
func (s *Service) processQueued(documentID int64) {
	ctx := context.Background()
	defer func() {
		if r := recover(); r != nil {
			s.log.ErrorContext(ctx, "queued extraction panicked", "document_id", documentID, "panic", fmt.Sprint(r))
			if err := s.repo.UpdateDocumentProcessing(ctx, documentID, domain.DocumentAnalysisFailed, "extraction crashed unexpectedly", 0, ""); err != nil {
				s.log.ErrorContext(ctx, "mark document processing failed", "document_id", documentID, "error", err)
			}
		}
	}()
	processing, err := s.ProcessDocument(ctx, documentID)
	if err != nil {
		s.log.ErrorContext(ctx, "queued extraction failed", "document_id", documentID, "error", err)
		return
	}
	s.log.InfoContext(ctx, "queued extraction finished", "document_id", documentID, "status", processing.Status,
		"published", processing.PublishedCount, "ignored", processing.IgnoredCount, "invalid", processing.InvalidCount)
}

// ProcessingResult is what one extraction run did to a material. It is
// returned to the background worker, which only logs it.
type ProcessingResult struct {
	RunID          string                 `json:"run_id,omitempty"`
	DocumentID     int64                  `json:"document_id"`
	Status         string                 `json:"status"`
	Published      []domain.KnowledgeItem `json:"published"`
	PublishedCount int                    `json:"published_count"`
	// IgnoredCount is how many drafts were dropped for being below the
	// confidence floor or for restating knowledge already in the base.
	IgnoredCount int `json:"ignored_count"`
	// InvalidCount is how many drafts failed validation: a kind outside
	// domain.Kinds, a field over its limit, or an evidence quote that is not a
	// contiguous slice of the stored text.
	InvalidCount int               `json:"invalid_count"`
	Reused       bool              `json:"reused"`
	TokenUsage   domain.TokenUsage `json:"token_usage"`
}

// ProcessDocument extracts knowledge from one stored material. It runs on the
// background queue; a document that already finished reuses its result.
//
// The whole material goes to the model in a single call, so every passage is
// read once -- there are no chunks and nothing to resume piecemeal.
func (s *Service) ProcessDocument(
	ctx context.Context,
	documentID int64,
) (processing ProcessingResult, returnErr error) {
	if documentID <= 0 {
		return processing, appError(CodeInvalidInput, "document id must be positive", nil)
	}
	processing.DocumentID = documentID
	document, err := s.repo.GetDocument(ctx, documentID)
	if err != nil {
		return processing, mapRepositoryError(err, CodeDocumentNotFound)
	}
	if document.Status == domain.DocumentPublished || document.Status == domain.DocumentProcessedEmpty {
		published, err := s.repo.ListKnowledgeByDocument(ctx, documentID)
		if err != nil {
			return processing, appError(CodeInternal, "read published knowledge failed", err)
		}
		processing.RunID = document.ProcessingRunID
		processing.Status = document.Status
		processing.Published = published
		processing.PublishedCount = len(published)
		processing.Reused = true
		return processing, nil
	}
	if document.Status == domain.DocumentProcessing {
		return processing, appError(CodeInvalidState, "document is already being processed", nil)
	}
	if configErr := s.ai.ConfigurationError(); configErr != nil {
		return processing, appError(CodeArkNotConfigured, configErr.Error(), configErr)
	}

	runID, err := newRunID()
	if err != nil {
		return processing, appError(CodeInternal, "generate agent run id failed", err)
	}
	processing.RunID = runID
	runStarted := time.Now()
	if err := s.repo.CreateAgentRun(ctx, domain.AgentRun{
		RunID: runID, Purpose: runPurposeKnowledgeIngestion, Status: domain.RunRunning,
	}); err != nil {
		return processing, appError(CodeInternal, "create knowledge ingestion run failed", err)
	}
	claimed := false
	// The document reaches a terminal state as soon as extraction is stored.
	// Vectors are built by the independent vector queue afterwards, so a later
	// failure there cannot downgrade the document back to analysis_failed.
	finalized := false

	defer func() {
		if returnErr != nil && claimed && !finalized {
			traceCtx, cancel := traceContext(ctx)
			_, errorMessage := errorFields(returnErr)
			if err := s.repo.UpdateDocumentProcessing(
				traceCtx, documentID, domain.DocumentAnalysisFailed, errorMessage, processing.PublishedCount, runID,
			); err != nil {
				s.log.ErrorContext(traceCtx, "mark document processing failed", "document_id", documentID, "run_id", runID, "error", err)
			}
			cancel()
		}
		if err := s.finishRun(ctx, runID, runPurposeKnowledgeIngestion, runStarted, processing.TokenUsage, returnErr); err != nil && returnErr == nil {
			processing = ProcessingResult{}
			returnErr = err
		}
		if returnErr != nil {
			returnErr = withRunID(returnErr, runID)
		}
	}()

	if err := s.repo.ClaimDocumentProcessing(ctx, documentID, runID); err != nil {
		return processing, mapRepositoryError(err, CodeDocumentNotFound)
	}
	claimed = true

	text, err := s.readStoredText(document)
	if err != nil {
		return processing, appError(CodeInternal, "read stored document text failed", err)
	}

	overrides, err := s.promptOverrides(ctx)
	if err != nil {
		return processing, err
	}
	system := prompts.Extraction(overrides)
	extractStarted := time.Now()
	drafts, usage, err := s.ai.ExtractCandidates(ctx, system.Text, text)
	processing.TokenUsage.Add(usage)
	if err != nil {
		mapped := mapArkError(err, "Ark knowledge extraction request failed", "Ark returned invalid knowledge data")
		if traceErr := s.addAgentEvent(ctx, runID, 1, "extract_knowledge", domain.RunFailed, extractStarted, mapped, map[string]any{
			"model": s.ai.Model(),
		}); traceErr != nil {
			return processing, traceErr
		}
		return processing, mapped
	}
	knowledge := make([]domain.KnowledgeDraft, 0, len(drafts))
	seen := make(map[string]struct{}, len(drafts))
	for _, draft := range drafts {
		item, ok := validateDraft(text, draft)
		if !ok {
			processing.InvalidCount++
			continue
		}
		// The confidence floor replaces the review step: a draft the model
		// itself is unsure about is dropped rather than stored.
		if draft.Confidence < s.minExtractConfidence {
			processing.IgnoredCount++
			continue
		}
		if _, duplicate := seen[item.Fingerprint]; duplicate {
			processing.IgnoredCount++
			continue
		}
		seen[item.Fingerprint] = struct{}{}
		knowledge = append(knowledge, item)
	}
	if err := s.addAgentEvent(ctx, runID, 1, "extract_knowledge", domain.RunSucceeded, extractStarted, nil, map[string]any{
		"candidate_count": len(drafts), "accepted_count": len(knowledge),
		"invalid_count": processing.InvalidCount, "model": s.ai.Model(), "prompt_version": system.PromptVersion(),
		"document_runes": utf8.RuneCountInString(text),
	}); err != nil {
		return processing, err
	}

	publishStarted := time.Now()
	published, ignored, err := s.repo.SaveDocumentKnowledge(ctx, documentID, knowledge)
	if err != nil {
		return processing, appError(CodeInternal, "save extracted knowledge failed", err)
	}
	processing.IgnoredCount += ignored
	total, err := s.repo.CountKnowledgeByDocument(ctx, documentID)
	if err != nil {
		return processing, appError(CodeInternal, "count document knowledge failed", err)
	}
	processing.PublishedCount = total
	processing.Status = domain.DocumentProcessedEmpty
	if processing.PublishedCount > 0 {
		processing.Status = domain.DocumentPublished
	}
	if err := s.repo.UpdateDocumentProcessing(
		ctx, documentID, processing.Status, "", processing.PublishedCount, runID,
	); err != nil {
		return processing, appError(CodeInternal, "finish document processing failed", err)
	}
	finalized = true
	if err := s.addAgentEvent(ctx, runID, 2, "publish_knowledge", domain.RunSucceeded, publishStarted, nil, map[string]any{
		"published_count":        processing.PublishedCount,
		"saved_count":            published,
		"ignored_count":          processing.IgnoredCount,
		"min_extract_confidence": s.minExtractConfidence,
	}); err != nil {
		return processing, err
	}

	publishedItems, err := s.repo.ListKnowledgeByDocument(ctx, documentID)
	if err != nil {
		return processing, appError(CodeInternal, "read published knowledge failed", err)
	}
	processing.Published = publishedItems

	s.log.InfoContext(ctx, "knowledge document processed", "document_id", documentID, "run_id", runID,
		"status", processing.Status, "published", processing.PublishedCount,
		"ignored", processing.IgnoredCount, "invalid", processing.InvalidCount)
	return processing, nil
}

// readStoredText reads the material's stored file and re-normalizes it to the
// exact text extraction reads, so stored evidence offsets stay recomputable.
func (s *Service) readStoredText(document *domain.Document) (string, error) {
	data, err := os.ReadFile(document.StoredPath)
	if err != nil {
		return "", err
	}
	return extractor.NormalizeText(data)
}

// validateDraft enforces the contract the extraction prompt states: a fixed
// kind, non-empty fields within their limits, and an evidence quote that
// appears verbatim in the stored text. Anywhere else is dropped rather than
// stored. Offsets are recomputed here, never trusted from the model.
func validateDraft(text string, draft domain.CandidateDraft) (domain.KnowledgeDraft, bool) {
	draft.Kind = strings.ToLower(strings.TrimSpace(draft.Kind))
	draft.CanonicalKey = strings.TrimSpace(draft.CanonicalKey)
	draft.Title = strings.TrimSpace(draft.Title)
	draft.Body = strings.TrimSpace(draft.Body)
	draft.EvidenceQuote = strings.TrimSpace(draft.EvidenceQuote)
	if !domain.AllowsKind(draft.Kind) || draft.CanonicalKey == "" || draft.Title == "" || draft.Body == "" || draft.EvidenceQuote == "" {
		return domain.KnowledgeDraft{}, false
	}
	if utf8.RuneCountInString(draft.CanonicalKey) > 240 || utf8.RuneCountInString(draft.Title) > 240 ||
		utf8.RuneCountInString(draft.Body) > 4000 || utf8.RuneCountInString(draft.EvidenceQuote) > 2000 || len(draft.Metadata) > 16<<10 {
		return domain.KnowledgeDraft{}, false
	}
	if math.IsNaN(draft.Confidence) || math.IsInf(draft.Confidence, 0) || draft.Confidence < 0 || draft.Confidence > 1 {
		return domain.KnowledgeDraft{}, false
	}
	byteIndex := strings.Index(text, draft.EvidenceQuote)
	if byteIndex < 0 {
		return domain.KnowledgeDraft{}, false
	}
	quoteStart := utf8.RuneCountInString(text[:byteIndex])
	quoteEnd := quoteStart + utf8.RuneCountInString(draft.EvidenceQuote)
	return domain.KnowledgeDraft{
		Kind: draft.Kind, Title: draft.Title, Body: draft.Body,
		Tags: normalizeTags(draft.Tags), Metadata: normalizeMetadata(draft.Metadata),
		Fingerprint:   knowledgeFingerprint(draft.Kind, draft.CanonicalKey),
		EvidenceQuote: draft.EvidenceQuote, QuoteStart: quoteStart, QuoteEnd: quoteEnd,
	}, true
}

// normalizeTags trims, de-duplicates and caps the free-form tags. They are
// labels for browsing and keyword matching, so a malformed one is dropped
// instead of failing the whole knowledge item.
func normalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || utf8.RuneCountInString(tag) > 32 {
			continue
		}
		key := strings.ToLower(tag)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, tag)
		if len(normalized) == domain.MaxTags {
			break
		}
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

// normalizeMetadata always yields a JSON object, because downstream tooling
// treats metadata as optional auxiliary data rather than a required field.
func normalizeMetadata(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	var metadata map[string]any
	if err := json.Unmarshal(raw, &metadata); err != nil || metadata == nil {
		return json.RawMessage("{}")
	}
	return raw
}

// knowledgeFingerprint identifies "the same knowledge". There is no longer any
// merging across materials: an item belongs to the one material it was
// extracted from, and the fingerprint is only used to keep reprocessing the
// same material from inserting the same row twice. The unique index is on
// (document_id, fingerprint), so the same claim in two materials, or in a new
// version of the same source, is kept as separate items.
func knowledgeFingerprint(kind, canonicalKey string) string {
	payload := strings.Join([]string{
		normalizeKnowledgeKey(kind), normalizeKnowledgeKey(canonicalKey),
	}, "\x00")
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func normalizeKnowledgeKey(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

// mapArkError turns Ark client failures into stable codes. An invalid key, a
// rate limit, and a timeout each need a different operator action, so they
// are not folded into one generic upstream error.
func mapArkError(err error, upstreamMessage, invalidMessage string) error {
	if errors.Is(err, ark.ErrInvalidResponse) {
		return appError(CodeArkInvalidResponse, invalidMessage+": "+strings.TrimPrefix(err.Error(), ark.ErrInvalidResponse.Error()+": "), err)
	}
	var upstream *ark.UpstreamError
	if errors.As(err, &upstream) {
		switch {
		case upstream.Timeout:
			return appError(CodeArkTimeout, "Ark request timed out; retry later or raise ARK_TIMEOUT_SECONDS", err)
		case upstream.StatusCode == http.StatusUnauthorized || upstream.StatusCode == http.StatusForbidden:
			return appError(CodeArkAuthFailed, "Ark rejected the API key or model access: "+upstream.Error(), err)
		case upstream.StatusCode == http.StatusTooManyRequests:
			return appError(CodeArkRateLimited, "Ark rate limit or quota was reached: "+upstream.Error(), err)
		}
		return appError(CodeArkUpstream, upstreamMessage+": "+upstream.Error(), err)
	}
	return appError(CodeArkUpstream, upstreamMessage, err)
}
