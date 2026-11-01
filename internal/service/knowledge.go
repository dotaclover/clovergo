package service

import (
	"context"
	"errors"
	"strings"

	"clovergo/internal/domain"
	"clovergo/internal/repository"
	localtools "clovergo/internal/tools"
)

// ListDocuments returns every material in the library, newest first.
func (s *Service) ListDocuments(ctx context.Context) ([]domain.Document, error) {
	documents, err := s.repo.ListDocuments(ctx)
	if err != nil {
		return nil, appError(CodeInternal, "list documents failed", err)
	}
	return documents, nil
}

func (s *Service) GetDocument(ctx context.Context, id int64) (*domain.Document, error) {
	if id <= 0 {
		return nil, appError(CodeInvalidInput, "document id must be positive", nil)
	}
	document, err := s.repo.GetDocument(ctx, id)
	if err != nil {
		return nil, mapRepositoryError(err, CodeDocumentNotFound)
	}
	return document, nil
}

// KnowledgeScope narrows a listing or a search to one shelf (Topic) and/or one
// language. Both are exact matches chosen by the caller, never inferred here.
// An empty field means "do not narrow on this axis".
type KnowledgeScope struct {
	Topic    string
	Language string
}

func (s KnowledgeScope) empty() bool {
	return s.Topic == "" && s.Language == ""
}

func (s KnowledgeScope) filter() repository.KnowledgeFilter {
	return repository.KnowledgeFilter{Topic: s.Topic, Language: s.Language}
}

// KnowledgeQuery is one request for the knowledge list: what to narrow on, and
// how many rows to return.
type KnowledgeQuery struct {
	Kind     string
	Topic    string
	Language string
	Usage    string
	Limit    int
	Offset   int
}

// ListKnowledge returns confirmed knowledge, optionally narrowed to one kind,
// one topic, one language, and whether a platform draft already cites it. It
// also returns the total matching the same filters so the list can page.
func (s *Service) ListKnowledge(ctx context.Context, query KnowledgeQuery) ([]domain.KnowledgeItem, int, error) {
	kind := domain.NormalizeKind(query.Kind)
	if kind != "" && !domain.ValidKind(kind) {
		return nil, 0, appError(CodeInvalidInput, "kind must be a label of at most 24 characters", nil)
	}
	usage := strings.ToLower(strings.TrimSpace(query.Usage))
	if usage == "all" {
		usage = repository.UsageAll
	}
	if usage != repository.UsageAll && usage != repository.UsageUnused && usage != repository.UsageUsed {
		return nil, 0, appError(CodeInvalidInput, "usage must be all, unused, or used", nil)
	}
	scope := KnowledgeScope{Topic: strings.TrimSpace(query.Topic), Language: strings.TrimSpace(query.Language)}
	filter := repository.KnowledgeFilter{
		Kind: kind, Topic: scope.Topic, Language: scope.Language,
		Usage: usage, Limit: normalizeLimit(query.Limit, 20), Offset: normalizeOffset(query.Offset),
	}
	items, err := s.repo.ListKnowledge(ctx, filter)
	if err != nil {
		return nil, 0, appError(CodeInternal, "list knowledge failed", err)
	}
	total, err := s.repo.CountKnowledge(ctx, filter)
	if err != nil {
		return nil, 0, appError(CodeInternal, "count knowledge failed", err)
	}
	return items, total, nil
}

// Kinds is the taxonomy the UI should offer: the built-in categories followed
// by any custom kind an operator has typed onto an item, so a custom kind can
// be filtered and picked again. Custom kinds carry their id as the label.
func (s *Service) Kinds(ctx context.Context) ([]domain.Kind, error) {
	used, err := s.repo.ListKindIDs(ctx)
	if err != nil {
		return nil, appError(CodeInternal, "list knowledge kinds failed", err)
	}
	kinds := make([]domain.Kind, 0, len(domain.Kinds)+len(used))
	kinds = append(kinds, domain.Kinds...)
	for _, id := range used {
		if !domain.AllowsKind(id) {
			kinds = append(kinds, domain.Kind{ID: id, Label: id})
		}
	}
	return kinds, nil
}

// ListTopics returns the shelves in use and how much knowledge stands on each,
// so the knowledge page can offer a filter without a second round trip.
func (s *Service) ListTopics(ctx context.Context) ([]repository.TopicSummary, error) {
	topics, err := s.repo.ListTopics(ctx)
	if err != nil {
		return nil, appError(CodeInternal, "list topics failed", err)
	}
	return topics, nil
}

// UpdateKnowledge rewrites the operator-editable fields of one item. The
// vector for it is left stale on purpose: it is keyed by a hash of title and
// body, so the very next reindex pass recomputes exactly this item and nothing
// else.
func (s *Service) UpdateKnowledge(ctx context.Context, id int64, edit repository.KnowledgeEdit) (*domain.KnowledgeItem, error) {
	if id <= 0 {
		return nil, appError(CodeInvalidInput, "knowledge id must be positive", nil)
	}
	edit.Kind = domain.NormalizeKind(edit.Kind)
	if !domain.ValidKind(edit.Kind) {
		return nil, appError(CodeInvalidInput, "kind must be a label of at most 24 characters", nil)
	}
	edit.Title = strings.TrimSpace(edit.Title)
	edit.Body = strings.TrimSpace(edit.Body)
	if edit.Title == "" {
		return nil, appError(CodeInvalidInput, "title is required", nil)
	}
	if edit.Body == "" {
		return nil, appError(CodeInvalidInput, "body is required", nil)
	}
	edit.Tags = normalizeTags(edit.Tags)
	item, err := s.repo.UpdateKnowledge(ctx, id, edit)
	if err != nil {
		return nil, mapRepositoryError(err, CodeKnowledgeNotFound)
	}
	s.log.InfoContext(ctx, "knowledge updated", "knowledge_id", id)
	return item, nil
}

// GetKnowledge reads one item. The draft screen uses it to show which item was
// handed over from the knowledge list, so the creator can see what the article
// will be written from before spending a model call.
func (s *Service) GetKnowledge(ctx context.Context, id int64) (*domain.KnowledgeItem, error) {
	if id <= 0 {
		return nil, appError(CodeInvalidInput, "knowledge id must be positive", nil)
	}
	items, err := s.repo.GetKnowledgeByIDs(ctx, []int64{id})
	if err != nil {
		return nil, appError(CodeInternal, "read knowledge failed", err)
	}
	if len(items) == 0 {
		return nil, appError(CodeKnowledgeNotFound, "knowledge was not found", nil)
	}
	return &items[0], nil
}

// MarkKnowledgeRead records that the owner has read one item, and returns the
// new total. The count is what makes re-reading visible: an item that keeps
// coming back to the top of the list is one worth knowing by heart.
func (s *Service) MarkKnowledgeRead(ctx context.Context, id int64) (int, error) {
	if id <= 0 {
		return 0, appError(CodeInvalidInput, "knowledge id must be positive", nil)
	}
	count, err := s.repo.IncrementKnowledgeRead(ctx, id)
	if err != nil {
		return 0, mapRepositoryError(err, CodeKnowledgeNotFound)
	}
	return count, nil
}

// DeleteKnowledge removes one item. Drafts do not record their sources, so
// nothing stops the delete; its vector and FTS row follow the item out.
func (s *Service) DeleteKnowledge(ctx context.Context, id int64) error {
	if id <= 0 {
		return appError(CodeInvalidInput, "knowledge id must be positive", nil)
	}
	if err := s.repo.DeleteKnowledge(ctx, id); err != nil {
		return mapRepositoryError(err, CodeKnowledgeNotFound)
	}
	s.log.InfoContext(ctx, "knowledge deleted", "knowledge_id", id)
	return nil
}

func (s *Service) ListKnowledgeByDocument(ctx context.Context, documentID int64) ([]domain.KnowledgeItem, error) {
	if documentID <= 0 {
		return nil, appError(CodeInvalidInput, "document id must be positive", nil)
	}
	items, err := s.repo.ListKnowledgeByDocument(ctx, documentID)
	if err != nil {
		return nil, appError(CodeInternal, "list document knowledge failed", err)
	}
	return items, nil
}

// Search finds current knowledge, optionally restricted to one shelf. The same
// scope narrows both retrievers, so a search inside 日语 cannot come back with
// an English item that merely scores well.
func (s *Service) Search(ctx context.Context, query string, limit int, scope KnowledgeScope) ([]domain.KnowledgeItem, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, appError(CodeInvalidInput, "search query is required", nil)
	}
	// Interactive search runs outside any Run, so query-embedding tokens are
	// not booked to the usage ledger here.
	retrieved, err := s.retrieve(ctx, query, normalizeLimit(limit, 20), scope)
	if err != nil {
		return nil, err
	}
	return retrieved.Items, nil
}

func (s *Service) SearchSources(ctx context.Context, input localtools.SearchSourcesInput) ([]domain.KnowledgeItem, error) {
	items, err := s.localTools.SearchSources(ctx, input)
	if err != nil {
		return nil, mapLocalToolError(err)
	}
	return items, nil
}

// GetSourceDocument returns one material's stored text, so a caller that
// followed a citation back to its origin can read the evidence in context.
func (s *Service) GetSourceDocument(ctx context.Context, documentID int64) (domain.SourceDocument, error) {
	if documentID <= 0 {
		return domain.SourceDocument{}, appError(CodeInvalidInput, "document id must be positive", nil)
	}
	document, err := s.repo.GetDocument(ctx, documentID)
	if err != nil {
		return domain.SourceDocument{}, mapRepositoryError(err, CodeDocumentNotFound)
	}
	text, err := s.readStoredText(document)
	if err != nil {
		return domain.SourceDocument{}, appError(CodeInternal, "read stored document text failed", err)
	}
	return domain.SourceDocument{
		DocumentID: document.ID, DocumentName: document.Filename, Topic: document.Topic, Text: text,
	}, nil
}

func mapLocalToolError(err error) error {
	switch {
	case errors.Is(err, localtools.ErrInvalidInput):
		return appError(CodeToolInputInvalid, "local tool input is invalid", err)
	case errors.Is(err, repository.ErrNotFound):
		return appError(CodeSourceNotFound, "confirmed source was not found", err)
	default:
		return appError(CodeInternal, "local knowledge tool failed", err)
	}
}

func normalizeLimit(limit, fallback int) int {
	if limit <= 0 {
		return fallback
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func normalizeOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}

// currentKnowledge strips evidence from superseded document versions so the
// model only sees text that is still in force. Items left without current
// evidence are dropped; dropped reports how many.
func currentKnowledge(items []domain.KnowledgeItem) (current []domain.KnowledgeItem, dropped int) {
	current = make([]domain.KnowledgeItem, 0, len(items))
	for _, item := range items {
		evidence := make([]domain.Evidence, 0, len(item.Evidence))
		for _, entry := range item.Evidence {
			if !entry.Superseded {
				evidence = append(evidence, entry)
			}
		}
		if len(evidence) == 0 {
			dropped++
			continue
		}
		item.Evidence = evidence
		current = append(current, item)
	}
	return current, dropped
}
