package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"clovergo/internal/domain"
)

// currentKnowledgeClause keeps knowledge (aliased k) that came from a material
// no newer processed version of the same source has replaced. Older versions
// stay stored and exportable, but retrieval no longer mixes an outdated policy
// with its replacement.
const currentKnowledgeClause = `NOT EXISTS (
	SELECT 1 FROM documents nd JOIN documents cd ON cd.id = k.document_id
	WHERE nd.source_key = cd.source_key AND nd.version > cd.version
		AND nd.status IN ('published', 'processed_empty'))`

// draftUsageClause is true when knowledge k has been written into at least one
// draft. The counter lives on the knowledge row because drafts no longer record
// which items they were written from.
const draftUsageClause = `k.use_count > 0`

// knowledgeFilterClause builds the WHERE suffix shared by the knowledge list,
// its count and the random pick, so a page and its total always describe the
// same set. It starts with " AND", ready to append after "... WHERE " +
// currentKnowledgeClause.
func knowledgeFilterClause(filter KnowledgeFilter) (string, []any, error) {
	clause := ""
	args := make([]any, 0, 5)
	if filter.Kind != "" {
		clause += " AND k.kind = ?"
		args = append(args, filter.Kind)
	}
	scopeClause, scopeArgs := knowledgeScopeClause(filter)
	clause += scopeClause
	args = append(args, scopeArgs...)
	switch filter.Usage {
	case UsageUnused:
		clause += " AND NOT " + draftUsageClause
	case UsageUsed:
		clause += " AND " + draftUsageClause
	case UsageAll:
	default:
		return "", nil, fmt.Errorf("unknown knowledge usage filter %q", filter.Usage)
	}
	return clause, args, nil
}

func (s *SQLite) ListKnowledge(ctx context.Context, filter KnowledgeFilter) ([]domain.KnowledgeItem, error) {
	clause, args, err := knowledgeFilterClause(filter)
	if err != nil {
		return nil, err
	}
	query := "SELECT k.id FROM knowledge_items k WHERE " + currentKnowledgeClause + clause +
		" ORDER BY k.id DESC LIMIT ?"
	args = append(args, filter.Limit)
	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}
	ids, err := s.queryIDs(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return s.knowledgeByIDs(ctx, ids)
}

// CountKnowledge returns how many items match the filter, ignoring Limit and
// Offset, so a pager can show the total of the same set it is paging.
func (s *SQLite) CountKnowledge(ctx context.Context, filter KnowledgeFilter) (int, error) {
	clause, args, err := knowledgeFilterClause(filter)
	if err != nil {
		return 0, err
	}
	var total int
	query := "SELECT COUNT(*) FROM knowledge_items k WHERE " + currentKnowledgeClause + clause
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

// ListKindIDs returns each kind that at least one current knowledge item
// carries, alphabetically. The built-in taxonomy lives in domain.Kinds; this
// query only reports the custom kinds an operator has typed onto items.
func (s *SQLite) ListKindIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT DISTINCT k.kind FROM knowledge_items k WHERE "+currentKnowledgeClause+" AND k.kind <> '' ORDER BY k.kind")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// RandomKnowledgeID returns one item matching the filter, picked by SQLite's
// RANDOM() rather than by any fixed order. Draft creation uses it to keep
// moving through knowledge no draft has cited yet, instead of always starting
// from the newest item. ErrNotFound means the filter matched nothing.
func (s *SQLite) RandomKnowledgeID(ctx context.Context, filter KnowledgeFilter) (int64, error) {
	clause, args, err := knowledgeFilterClause(filter)
	if err != nil {
		return 0, err
	}
	query := "SELECT k.id FROM knowledge_items k WHERE " + currentKnowledgeClause + clause +
		" ORDER BY RANDOM() LIMIT 1"
	var id int64
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return id, nil
}

// IncrementKnowledgeRead records one more read of a knowledge item and returns
// the new total. It is deliberately a bump rather than a read-then-write: the
// row is updated in a single statement, so two tabs open at once cannot lose
// a count.
func (s *SQLite) IncrementKnowledgeRead(ctx context.Context, id int64) (int, error) {
	result, err := s.db.ExecContext(ctx,
		`UPDATE knowledge_items SET read_count = read_count + 1 WHERE id = ?`, id)
	if err != nil {
		return 0, fmt.Errorf("increment knowledge read: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected == 0 {
		return 0, ErrNotFound
	}
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT read_count FROM knowledge_items WHERE id = ?`, id).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// IncrementKnowledgeUse records that a draft was written from each of these
// items. One statement keeps the counters in step with the draft that was
// actually saved; unknown ids are ignored.
func (s *SQLite) IncrementKnowledgeUse(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE knowledge_items SET use_count = use_count + 1 WHERE id IN (`+strings.Join(placeholders, ",")+`)`,
		args...); err != nil {
		return fmt.Errorf("increment knowledge use: %w", err)
	}
	return nil
}

// UpdateKnowledge rewrites the editable fields of one knowledge item. FTS
// stays in step on its own: the knowledge_au trigger rewrites the index row
// whenever knowledge_items changes. The vector does not, and is not meant to --
// it is keyed by a hash of title+body, so the edit itself marks it stale and
// the next reindex pass picks it up.
func (s *SQLite) UpdateKnowledge(ctx context.Context, id int64, edit KnowledgeEdit) (*domain.KnowledgeItem, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE knowledge_items
		SET kind = ?, title = ?, body = ?, tags_json = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, edit.Kind, edit.Title, edit.Body, encodeTags(edit.Tags), id)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if changed != 1 {
		return nil, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	items, err := s.knowledgeByIDs(ctx, []int64{id})
	if err != nil {
		return nil, err
	}
	if len(items) != 1 {
		return nil, ErrNotFound
	}
	return &items[0], nil
}

// DeleteKnowledge removes one knowledge item. Its vector follows automatically
// (ON DELETE CASCADE) and the FTS row is dropped by the knowledge_ad trigger,
// so the whole item goes in one step.
func (s *SQLite) DeleteKnowledge(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM knowledge_items WHERE id = ?", id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) ListKnowledgeByDocument(ctx context.Context, documentID int64) ([]domain.KnowledgeItem, error) {
	ids, err := s.queryIDs(ctx, `SELECT id FROM knowledge_items
		WHERE document_id = ? ORDER BY id`, documentID)
	if err != nil {
		return nil, err
	}
	return s.knowledgeByIDs(ctx, ids)
}

func (s *SQLite) GetKnowledgeByIDs(ctx context.Context, ids []int64) ([]domain.KnowledgeItem, error) {
	if len(ids) == 0 {
		return []domain.KnowledgeItem{}, nil
	}
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, fmt.Errorf("knowledge item id must be positive: %d", id)
		}
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("duplicate knowledge item id: %d", id)
		}
		seen[id] = struct{}{}
	}
	items, err := s.knowledgeByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	if len(items) != len(ids) {
		return nil, fmt.Errorf("%w: one or more knowledge items do not exist", ErrNotFound)
	}
	return items, nil
}

// knowledgeScopeClause narrows knowledge (aliased k) to the shelves a filter
// names, by looking at the material the item was extracted from. An item belongs
// to exactly one material now, so both axes are simple lookups on that
// document.
//
// The returned arguments are positional and belong immediately after
// currentKnowledgeClause, which takes none of its own.
func knowledgeScopeClause(filter KnowledgeFilter) (string, []any) {
	clause := ""
	args := make([]any, 0, 2)
	if filter.Topic != "" {
		clause += ` AND EXISTS (SELECT 1 FROM documents td WHERE td.id = k.document_id AND td.topic = ?)`
		args = append(args, filter.Topic)
	}
	if filter.Language != "" {
		clause += ` AND EXISTS (SELECT 1 FROM documents ld WHERE ld.id = k.document_id AND ld.language = ?)`
		args = append(args, filter.Language)
	}
	return clause, args
}

// KnowledgeIDsInScope lists every current knowledge item on the shelves a
// filter names, so retrieval can restrict its rankings to them. Callers pass a
// filter that actually narrows something; an empty filter would return the
// whole library.
func (s *SQLite) KnowledgeIDsInScope(ctx context.Context, filter KnowledgeFilter) ([]int64, error) {
	scopeClause, args := knowledgeScopeClause(filter)
	return s.queryIDs(ctx, "SELECT k.id FROM knowledge_items k WHERE "+currentKnowledgeClause+scopeClause, args...)
}

// SearchKnowledgeIDs ranks current knowledge with FTS5 first and then fills the
// remaining slots with LIKE matches scored by how many query terms they hit.
// The LIKE pass is what finds two-character CJK words, which the trigram
// tokenizer cannot match, and it also matches tags. A non-empty filter keeps
// only hits on the shelves it names.
func (s *SQLite) SearchKnowledgeIDs(ctx context.Context, query string, limit int, filter KnowledgeFilter) ([]int64, error) {
	terms := searchTerms(query)
	if len(terms.like) == 0 {
		return []int64{}, nil
	}
	scopeClause, scopeArgs := knowledgeScopeClause(filter)
	ids := make([]int64, 0, limit)
	ftsTerms := make([]string, 0, len(terms.fts))
	for _, term := range terms.fts {
		switch s.ftsMode {
		case "fts5-trigram":
			if utf8.RuneCountInString(term) >= 3 {
				ftsTerms = append(ftsTerms, term)
			}
		case "fts5-unicode61":
			// unicode61 indexes a whole CJK run as one token, so only
			// space-delimited words can match reliably.
			if isASCII(term) {
				ftsTerms = append(ftsTerms, term)
			}
		}
	}
	if len(ftsTerms) > 0 {
		quoted := make([]string, 0, len(ftsTerms))
		for _, term := range ftsTerms {
			quoted = append(quoted, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
		}
		sqlText := `SELECT k.id FROM knowledge_fts f JOIN knowledge_items k ON k.id = f.rowid
			WHERE knowledge_fts MATCH ? AND ` + currentKnowledgeClause + scopeClause +
			" ORDER BY bm25(knowledge_fts), k.id DESC LIMIT ?"
		ftsArgs := make([]any, 0, len(scopeArgs)+2)
		ftsArgs = append(ftsArgs, strings.Join(quoted, " OR "))
		ftsArgs = append(ftsArgs, scopeArgs...)
		ftsArgs = append(ftsArgs, limit)
		ftsIDs, err := s.queryIDs(ctx, sqlText, ftsArgs...)
		if err != nil {
			return nil, fmt.Errorf("full-text search: %w", err)
		}
		ids = append(ids, ftsIDs...)
	}
	if len(ids) < limit {
		scores := make([]string, 0, len(terms.like)*3)
		args := make([]any, 0, len(terms.like)*3+len(scopeArgs)+len(ids)+1)
		for _, term := range terms.like {
			pattern := "%" + escapeLike(term) + "%"
			scores = append(scores, `(k.title LIKE ? ESCAPE '\')`, `(k.body LIKE ? ESCAPE '\')`, `(k.tags_json LIKE ? ESCAPE '\')`)
			args = append(args, pattern, pattern, pattern)
		}
		sqlText := "SELECT id FROM (SELECT k.id AS id, " + strings.Join(scores, " + ") +
			" AS score FROM knowledge_items k WHERE " + currentKnowledgeClause + scopeClause
		args = append(args, scopeArgs...)
		if len(ids) > 0 {
			sqlText += " AND k.id NOT IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"
			for _, id := range ids {
				args = append(args, id)
			}
		}
		sqlText += ") WHERE score > 0 ORDER BY score DESC, id DESC LIMIT ?"
		args = append(args, limit-len(ids))
		likeIDs, err := s.queryIDs(ctx, sqlText, args...)
		if err != nil {
			return nil, fmt.Errorf("keyword search: %w", err)
		}
		ids = append(ids, likeIDs...)
	}
	return ids, nil
}

// SearchKnowledge is the keyword-only search used by the local tools.
func (s *SQLite) SearchKnowledge(ctx context.Context, query string, limit int) ([]domain.KnowledgeItem, error) {
	ids, err := s.SearchKnowledgeIDs(ctx, query, limit, KnowledgeFilter{})
	if err != nil {
		return nil, err
	}
	return s.knowledgeByIDs(ctx, ids)
}

type queryTerms struct {
	fts  []string
	like []string
}

const maxSearchTerms = 24

var (
	englishStopwords = map[string]bool{
		"the": true, "and": true, "what": true, "which": true, "does": true, "how": true, "are": true, "is": true,
		"with": true, "from": true, "about": true, "this": true, "that": true, "for": true, "can": true,
	}
	// Question words carry no topic; removing them keeps n-grams such as
	// 程是什 out of the query.
	cjkQuestionWords = strings.NewReplacer(
		"请问", " ", "是什么", " ", "什么", " ", "怎么样", " ", "怎么", " ", "如何", " ", "有哪些", " ", "哪些", " ",
		"哪个", " ", "为什么", " ", "是否", " ", "可以吗", " ", "需要吗", " ", "吗", " ", "呢", " ", "的", " ",
		"以及", " ", "与", " ", "及", " ",
		"とは", " ", "ですか", " ", "ますか", " ", "について", " ", "教えて", " ",
	)
)

func searchTerms(query string) queryTerms {
	clean := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, query)
	clean = cjkQuestionWords.Replace(clean)

	var terms queryTerms
	seenFTS, seenLike := map[string]bool{}, map[string]bool{}
	addFTS := func(term string) {
		if !seenFTS[term] && len(terms.fts) < maxSearchTerms {
			seenFTS[term] = true
			terms.fts = append(terms.fts, term)
		}
	}
	addLike := func(term string) {
		if !seenLike[term] && len(terms.like) < maxSearchTerms {
			seenLike[term] = true
			terms.like = append(terms.like, term)
		}
	}
	for _, field := range strings.Fields(clean) {
		runes := []rune(field)
		if isASCII(field) {
			if len(runes) >= 2 && !englishStopwords[field] {
				addFTS(field)
				addLike(field)
			}
			continue
		}
		// Whole short phrases score highest when they match verbatim.
		if len(runes) <= 12 {
			addFTS(field)
			addLike(field)
		}
		for i := 0; i+3 <= len(runes); i++ {
			addFTS(string(runes[i : i+3]))
		}
		for i := 0; i+2 <= len(runes); i++ {
			addLike(string(runes[i : i+2]))
		}
	}
	return terms
}

func isASCII(value string) bool {
	for _, r := range value {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

func (s *SQLite) queryIDs(ctx context.Context, query string, args ...any) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// decodeTags turns the stored JSON array into a slice, tolerating rows written
// before tags existed.
func decodeTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return []string{}
	}
	kept := make([]string, 0, len(tags))
	for _, tag := range tags {
		if trimmed := strings.TrimSpace(tag); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}
	return kept
}

func encodeTags(tags []string) string {
	if len(tags) == 0 {
		return "[]"
	}
	data, err := json.Marshal(tags)
	if err != nil {
		return "[]"
	}
	return string(data)
}

// KnowledgeIDsByDocumentIDs lists current knowledge produced by each of the
// given materials, so document-level semantic retrieval can boost the
// knowledge that came out of a material the query matched. Documents that
// produced no knowledge simply have no entry.
func (s *SQLite) KnowledgeIDsByDocumentIDs(ctx context.Context, documentIDs []int64) (map[int64][]int64, error) {
	if len(documentIDs) == 0 {
		return map[int64][]int64{}, nil
	}
	query := "SELECT k.document_id, k.id FROM knowledge_items k WHERE k.document_id IN (" +
		placeholders(len(documentIDs)) + ") AND " + currentKnowledgeClause + " ORDER BY k.id"
	args := make([]any, len(documentIDs))
	for i, id := range documentIDs {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byDocument := make(map[int64][]int64, len(documentIDs))
	for rows.Next() {
		var documentID, knowledgeID int64
		if err := rows.Scan(&documentID, &knowledgeID); err != nil {
			return nil, err
		}
		byDocument[documentID] = append(byDocument[documentID], knowledgeID)
	}
	return byDocument, rows.Err()
}

// knowledgeByIDs loads knowledge items with their inline evidence: one row per
// item, because the quote and its offsets live on the knowledge row itself.
func (s *SQLite) knowledgeByIDs(ctx context.Context, ids []int64) ([]domain.KnowledgeItem, error) {
	if len(ids) == 0 {
		return []domain.KnowledgeItem{}, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	orderCases := make([]string, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
		orderCases[i] = "WHEN " + strconv.FormatInt(id, 10) + " THEN " + strconv.Itoa(i)
	}
	query := `SELECT k.id, k.document_id, k.kind, k.title, k.body, k.tags_json, k.metadata_json,
		k.created_at, k.updated_at, k.evidence_quote, k.quote_start, k.quote_end,
		d.filename, d.topic, d.version,
		EXISTS (SELECT 1 FROM documents nd WHERE nd.source_key = d.source_key
			AND nd.version > d.version AND nd.status IN ('published', 'processed_empty')),
		k.use_count, k.read_count
		FROM knowledge_items k
		JOIN documents d ON d.id = k.document_id
		WHERE k.id IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY CASE k.id ` + strings.Join(orderCases, " ") + ` END`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.KnowledgeItem, 0, len(ids))
	for rows.Next() {
		var item domain.KnowledgeItem
		var tags, metadata, quote, documentName, documentTopic string
		var quoteStart, quoteEnd, documentVersion int64
		var superseded bool
		if err := rows.Scan(&item.ID, &item.DocumentID, &item.Kind, &item.Title,
			&item.Body, &tags, &metadata, &item.CreatedAt, &item.UpdatedAt,
			&quote, &quoteStart, &quoteEnd, &documentName, &documentTopic, &documentVersion,
			&superseded, &item.UseCount, &item.ReadCount); err != nil {
			return nil, err
		}
		item.Tags = decodeTags(tags)
		item.Metadata = []byte(metadata)
		item.Topics = []string{}
		if documentTopic != "" {
			item.Topics = append(item.Topics, documentTopic)
		}
		item.Evidence = []domain.Evidence{{
			DocumentID: item.DocumentID, DocumentName: documentName, Topic: documentTopic,
			Quote: quote, QuoteStart: int(quoteStart), QuoteEnd: int(quoteEnd),
			DocumentVersion: int(documentVersion), Superseded: superseded,
		}}
		items = append(items, item)
	}
	return items, rows.Err()
}
