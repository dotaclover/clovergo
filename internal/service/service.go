package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"unicode"

	"clovergo/internal/domain"
	"clovergo/internal/platforms"
	"clovergo/internal/repository"
	localtools "clovergo/internal/tools"
	"clovergo/internal/wechat"
)

const (
	CodeInvalidInput           = "INVALID_INPUT"
	CodeUnsupportedFormat      = "UNSUPPORTED_FORMAT"
	CodeFileTooLarge           = "FILE_TOO_LARGE"
	CodeDocumentEmpty          = "DOCUMENT_EMPTY"
	CodeDocumentNotFound       = "DOCUMENT_NOT_FOUND"
	CodeKnowledgeNotFound      = "KNOWLEDGE_NOT_FOUND"
	CodeInvalidState           = "INVALID_STATE"
	CodeArkNotConfigured       = "ARK_NOT_CONFIGURED"
	CodeArkUpstream            = "ARK_UPSTREAM_ERROR"
	CodeArkAuthFailed          = "ARK_AUTH_FAILED"
	CodeArkRateLimited         = "ARK_RATE_LIMITED"
	CodeArkTimeout             = "ARK_TIMEOUT"
	CodeArkInvalidResponse     = "ARK_INVALID_RESPONSE"
	CodeNoGroundedKnowledge    = "NO_GROUNDED_KNOWLEDGE"
	CodeToolInputInvalid       = "TOOL_INPUT_INVALID"
	CodeSourceNotFound         = "SOURCE_NOT_FOUND"
	CodeRootNotFound           = "ROOT_NOT_FOUND"
	CodeRunNotFound            = "RUN_NOT_FOUND"
	CodeMediaDraftNotFound     = "MEDIA_DRAFT_NOT_FOUND"
	CodeRevisionConflict       = "REVISION_CONFLICT"
	CodeEmbeddingNotConfigured = "EMBEDDING_NOT_CONFIGURED"
	CodeInternal               = "INTERNAL_ERROR"
	CodeRunInterrupted         = "RUN_INTERRUPTED"
	CodeForbiddenOrigin        = "FORBIDDEN_ORIGIN"
	CodeUnsupportedMediaType   = "UNSUPPORTED_MEDIA_TYPE"
	CodeAgentStepLimit         = "AGENT_STEP_LIMIT"
	CodeWechatNotConfigured    = "WECHAT_NOT_CONFIGURED"
	CodeWechatUpstream         = "WECHAT_UPSTREAM_ERROR"
	CodeAuthRequired           = "AUTH_REQUIRED"
	CodePasswordIncorrect      = "PASSWORD_INCORRECT"
	CodeTooManyAttempts        = "TOO_MANY_ATTEMPTS"
)

type AppError struct {
	Code    string
	Message string
	Cause   error
	RunID   string
}

func (e *AppError) Error() string { return e.Message }
func (e *AppError) Unwrap() error { return e.Cause }

func appError(code, message string, cause error) error {
	return &AppError{Code: code, Message: message, Cause: cause}
}

type AI interface {
	Configured() bool
	ConfigurationError() error
	Model() string
	// Every generation method receives the composed system prompt from
	// internal/prompts; the client never builds prompts itself.
	Summarize(ctx context.Context, system string, text string) (string, domain.TokenUsage, error)
	ExtractCandidates(ctx context.Context, system string, text string) ([]domain.CandidateDraft, domain.TokenUsage, error)
	Answer(ctx context.Context, system, question string, sources []domain.KnowledgeItem) (domain.AnswerDraft, domain.TokenUsage, error)
	DraftForPlatform(ctx context.Context, platform platforms.Platform, system string, input domain.MediaDraftInput, knowledge []domain.KnowledgeItem, documents []domain.SourceDocument) (domain.MediaDraftBody, domain.TokenUsage, error)
	// Chat is one free tool-calling turn for the agent loop.
	Chat(ctx context.Context, messages []domain.ChatMessage, tools []domain.ToolSpec, maxTokens int, answerOnly bool) (domain.ChatMessage, domain.TokenUsage, error)
}

// Embedder turns text into normalized vectors for semantic search. When it is
// not configured, retrieval is keyword-only and /health/ready says so.
type Embedder interface {
	Configured() bool
	Model() string
	Dimensions() int
	Embed(ctx context.Context, text string) ([]float32, domain.TokenUsage, error)
}

type Service struct {
	repo                 repository.Repository
	ai                   AI
	embedder             Embedder
	localTools           *localtools.Local
	log                  *slog.Logger
	libraryDir           string
	maxUploadBytes       int64
	maxDocumentRunes     int
	minExtractConfidence float64
	queue                *extractionQueue
	drafts               *draftQueue
	wechatDefaults       domain.WechatSettings
	lockGuard            lockGuard

	// The WeChat client is reused while the credentials and proxy stay the
	// same, so its access token is cached across pushes.
	wechatMu     sync.Mutex
	wechatClient *wechat.Client
	wechatKey    [3]string
	// pushing holds the draft IDs with a push in flight.
	pushMu  sync.Mutex
	pushing map[int64]struct{}
}

func New(
	repo repository.Repository,
	aiClient AI,
	embedder Embedder,
	log *slog.Logger,
	libraryDir string,
	maxUploadBytes int64,
	maxDocumentRunes int,
	minExtractConfidence float64,
) *Service {
	service := &Service{
		repo: repo, ai: aiClient, embedder: embedder, localTools: localtools.NewLocal(repo), log: log, libraryDir: libraryDir,
		maxUploadBytes: maxUploadBytes, maxDocumentRunes: maxDocumentRunes, minExtractConfidence: minExtractConfidence,
		pushing: make(map[int64]struct{}),
	}
	service.queue = newExtractionQueue(service)
	service.drafts = newDraftQueue(service)
	return service
}

// Start launches the background extraction and draft queues. Vectorization is
// manual and has no queue.
func (s *Service) Start(ctx context.Context) error {
	if err := s.queue.Start(ctx); err != nil {
		return err
	}
	return s.drafts.Start(ctx)
}

// Stop drains the background queues. In-flight work is released by
// RecoverInterruptedWork on the next start.
func (s *Service) Stop() {
	s.drafts.Stop()
	s.queue.Stop()
}

func (s *Service) Ready(ctx context.Context) error { return s.repo.Ping(ctx) }
func (s *Service) FTSMode() string                 { return s.repo.FTSMode() }
func (s *Service) MaxUploadBytes() int64           { return s.maxUploadBytes }
func (s *Service) ArkConfigured() bool             { return s.ai.Configured() }
func (s *Service) ArkModel() string                { return s.ai.Model() }
func (s *Service) EmbeddingConfigured() bool       { return s.embedder.Configured() }
func (s *Service) EmbeddingModel() string          { return s.embedder.Model() }

// QueueDepth is how many materials are waiting for background extraction.
func (s *Service) QueueDepth() int { return s.queue.Depth() }

// DraftQueueDepth is how many article drafts are waiting to be written.
func (s *Service) DraftQueueDepth() int { return s.drafts.Depth() }

// VectorPending is how many current knowledge items still lack a current
// embedding. Vectorization is an independent optional queue: items without a
// vector simply drop out of semantic ranking, and keyword search still finds
// them.
func (s *Service) VectorPending(ctx context.Context) (int, error) {
	_, pending, err := s.repo.VectorTasks(ctx, s.embedder.Model(), s.embedder.Dimensions(), 0, nil)
	if err != nil {
		return 0, appError(CodeInternal, "count pending vectors failed", err)
	}
	return pending, nil
}

// DocumentVectorPending is how many materials still lack a current summary
// vector. Like knowledge, materials are embedded by the same background queue,
// so a material without a vector simply drops out of document-level semantic
// ranking and stays reachable by keyword.
func (s *Service) DocumentVectorPending(ctx context.Context) (int, error) {
	_, pending, err := s.repo.DocumentVectorTasks(ctx, s.embedder.Model(), s.embedder.Dimensions(), 0)
	if err != nil {
		return 0, appError(CodeInternal, "count pending material vectors failed", err)
	}
	return pending, nil
}

// Stats reports whole-library size for the workspace header and the vector
// coverage page.
func (s *Service) Stats(ctx context.Context) (repository.LibraryStats, error) {
	stats, err := s.repo.LibraryStats(ctx, s.embedder.Model(), s.embedder.Dimensions())
	if err != nil {
		return repository.LibraryStats{}, appError(CodeInternal, "count library content failed", err)
	}
	return stats, nil
}

// detectLanguage guesses the language of imported material so the materials
// page can label it. It is display metadata only: extraction writes Chinese
// regardless of the source language.
func detectLanguage(text string) string {
	var kana, han, latin, total int
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		total++
		switch {
		case r >= 0x3040 && r <= 0x30FF:
			kana++
		case unicode.Is(unicode.Han, r):
			han++
		case unicode.Is(unicode.Latin, r):
			latin++
		}
	}
	if total == 0 {
		return domain.LanguageUnknown
	}
	switch {
	case kana*20 >= total:
		return domain.LanguageJapanese
	case han*4 >= total:
		return domain.LanguageChinese
	case latin*2 >= total:
		return domain.LanguageEnglish
	default:
		return domain.LanguageUnknown
	}
}

func mapRepositoryError(err error, notFoundCode string) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return appError(notFoundCode, "requested record was not found", err)
	case errors.Is(err, repository.ErrInvalidState):
		return appError(CodeInvalidState, "record is not in the required state", err)
	default:
		return appError(CodeInternal, "database operation failed", err)
	}
}
