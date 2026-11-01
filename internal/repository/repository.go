package repository

import (
	"context"
	"errors"

	"clovergo/internal/domain"
	"clovergo/internal/platforms"
)

var (
	ErrNotFound         = errors.New("record not found")
	ErrInvalidState     = errors.New("record is not in the required state")
	ErrRevisionConflict = errors.New("revision conflict: the record changed since it was read")
)

// Knowledge usage filters: whether any draft has been written from an item.
const (
	UsageAll    = ""
	UsageUnused = "unused"
	UsageUsed   = "used"
)

type KnowledgeFilter struct {
	Kind string
	// Topic narrows to knowledge that has at least one piece of evidence on a
	// material filed under this topic. Matching is exact, so 日语 and 日语语法
	// stay distinct shelves rather than collapsing into one another the way a
	// substring match on tags would.
	Topic string
	// Language narrows to knowledge of materials written in one language
	// (ja, en...). It is a separate axis from Topic on purpose.
	Language string
	Usage    string
	Limit    int
	// Offset skips this many matching rows, so a page and its total describe
	// the same set. It is ignored by CountKnowledge.
	Offset int
}

// TopicSummary is one shelf: the topic name and how many confirmed knowledge
// items stand on it.
type TopicSummary struct {
	Topic          string `json:"topic"`
	KnowledgeCount int    `json:"knowledge_count"`
}

// KnowledgeEdit carries the fields an operator may rewrite on one knowledge
// item. Identity, provenance and the material it was extracted from are not
// editable: they are what keeps the item traceable to a quote in a source.
type KnowledgeEdit struct {
	Kind  string
	Title string
	Body  string
	Tags  []string
}

type Repository interface {
	Close() error
	Ping(context.Context) error
	FTSMode() string

	// Documents. A material is stored whole: there is no chunk table.
	FindLatestDocumentBySource(context.Context, string) (*domain.Document, error)
	CreateDocument(context.Context, domain.Document) (*domain.Document, error)
	GetDocument(context.Context, int64) (*domain.Document, error)
	ListDocuments(context.Context) ([]domain.Document, error)
	UpdateDocumentTopic(context.Context, int64, string) (*domain.Document, error)
	RenameDocument(context.Context, int64, string) (*domain.Document, error)
	DeleteDocument(ctx context.Context, id int64, processingStatus string) (storedPath string, shared bool, err error)
	ListTopics(context.Context) ([]TopicSummary, error)
	ClaimDocumentProcessing(context.Context, int64, string) error
	UpdateDocumentProcessing(context.Context, int64, string, string, int, string) error
	RequeueDocument(ctx context.Context, id int64, processingStatus, readyStatus string) (bool, error)

	// Registered folders. Scanning is read-only; documents keep their rows
	// when a folder is forgotten.
	AddRoot(ctx context.Context, path, name string) (*domain.LibraryRoot, error)
	ListRoots(context.Context) ([]domain.LibraryRoot, error)
	GetRoot(context.Context, int64) (*domain.LibraryRoot, error)
	DeleteRoot(context.Context, int64) error
	ListRootDocuments(context.Context, int64) ([]domain.Document, error)
	MarkRootMissing(ctx context.Context, rootID int64, missingRelPaths []string) error

	// Knowledge extracted from a material, with its evidence inline.
	SaveDocumentKnowledge(context.Context, int64, []domain.KnowledgeDraft) (int, int, error)
	CountKnowledgeByDocument(context.Context, int64) (int, error)
	ListKnowledge(context.Context, KnowledgeFilter) ([]domain.KnowledgeItem, error)
	CountKnowledge(context.Context, KnowledgeFilter) (int, error)
	// ListKindIDs lists the kinds actually stored on knowledge, so a custom
	// kind an operator typed shows up in filters and pickers.
	ListKindIDs(context.Context) ([]string, error)
	ListKnowledgeByDocument(context.Context, int64) ([]domain.KnowledgeItem, error)
	UpdateKnowledge(context.Context, int64, KnowledgeEdit) (*domain.KnowledgeItem, error)
	DeleteKnowledge(context.Context, int64) error
	IncrementKnowledgeRead(context.Context, int64) (int, error)
	IncrementKnowledgeUse(context.Context, []int64) error
	IncrementDocumentUse(context.Context, []int64) error
	GetKnowledgeByIDs(context.Context, []int64) ([]domain.KnowledgeItem, error)
	SearchKnowledge(context.Context, string, int) ([]domain.KnowledgeItem, error)
	SearchKnowledgeIDs(context.Context, string, int, KnowledgeFilter) ([]int64, error)
	KnowledgeIDsInScope(context.Context, KnowledgeFilter) ([]int64, error)

	// Platform styles and vectors.
	PlatformSettings(context.Context, string) (platforms.Settings, int, bool, error)
	SavePlatformSettings(context.Context, string, int, platforms.Settings, string) (int, error)
	// Prompt overrides replace one built-in prompt layer (text embedded from
	// internal/prompts/layers) without a code change.
	PromptOverrides(context.Context) (map[string]domain.PromptOverride, error)
	SavePromptOverride(context.Context, string, string, int) (domain.PromptOverride, error)
	ResetPromptOverride(context.Context, string, int) error
	SaveKnowledgeVector(context.Context, VectorTask, string, []float32) error
	VectorTasks(context.Context, string, int, int, []int64) ([]VectorTask, int, error)
	CurrentVectors(context.Context, string, int) ([]KnowledgeVector, error)
	SaveDocumentVector(context.Context, DocumentVectorTask, string, []float32) error
	DocumentVectorTasks(context.Context, string, int, int) ([]DocumentVectorTask, int, error)
	CurrentDocumentVectors(context.Context, string, int) ([]DocumentVector, error)
	KnowledgeIDsByDocumentIDs(context.Context, []int64) (map[int64][]int64, error)
	LibraryStats(context.Context, string, int) (LibraryStats, error)

	// Self-media output built on the knowledge base. A draft is stored before
	// the model call, then completed or failed by the background worker.
	CreatePendingMediaDraft(context.Context, domain.MediaDraft, domain.Artifact) (*domain.MediaDraft, error)
	CompleteMediaDraft(context.Context, int64, domain.MediaDraft, string) (*domain.MediaDraft, error)
	FailMediaDraft(context.Context, int64, string) error
	GetMediaDraft(context.Context, int64) (*domain.MediaDraft, error)
	ListMediaDrafts(context.Context, string, int, int) ([]domain.MediaDraft, error)
	CountMediaDrafts(context.Context, string) (int, error)
	UpdateMediaDraftBody(context.Context, int64, domain.MediaDraftBody, string) (*domain.MediaDraft, error)
	DeleteMediaDraft(context.Context, int64) error
	MarkDraftPushed(context.Context, int64, string) error
	MarkDraftPublished(context.Context, int64, string) error

	// WeChat settings are the owner's own Official Account credentials and
	// defaults, stored in one row so the configuration screen can change them.
	WechatSettings(context.Context) (*domain.WechatSettings, error)
	SaveWechatSettings(context.Context, domain.WechatSettings) (*domain.WechatSettings, error)
	// The global lock password: nil means none is set.
	AppLock(context.Context) (*domain.AppLock, error)
	SaveAppLock(context.Context, domain.AppLock) error
	DeleteAppLock(context.Context) error
	// RandomKnowledgeID picks one knowledge item at random, so a draft with
	// no explicit source does not always start from the same end of the list.
	RandomKnowledgeID(context.Context, KnowledgeFilter) (int64, error)

	// Runs and usage.
	CreateWorkflowRun(context.Context, domain.WorkflowRun, domain.AgentRun) error
	CreateAgentRun(context.Context, domain.AgentRun) error
	AddAgentEvent(context.Context, domain.AgentEvent) error
	CompleteAgentRun(context.Context, string, string, int64, string, string, domain.TokenUsage) error
	GetAgentRun(context.Context, string) (*domain.AgentRun, error)
	ListAgentRuns(context.Context, int) ([]domain.AgentRun, error)
	SaveAgentQA(context.Context, domain.AgentQA) error
	ListAgentQA(context.Context, int, int) ([]domain.AgentQA, error)
	CountAgentQA(context.Context) (int, error)
	RecordUsage(context.Context, domain.UsageRecord) error
}
