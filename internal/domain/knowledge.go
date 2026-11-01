package domain

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Kind is one category of extracted knowledge.
//
// Kinds below are the built-in taxonomy, fixed here in code on purpose: it
// becomes the enum of the extraction tool schema, which is what keeps automatic
// classification stable. The model must pick one of these ids instead of
// inventing labels, so kind stays usable for browsing and filtering and can be
// part of the dedup fingerprint. Anything the model wants to express beyond
// this axis belongs in the free-form tags.
//
// An operator may still type a custom kind onto one item by hand; that is an
// edit decision, not an extraction one, and the extraction tool never emits
// anything outside the built-in ids.
type Kind struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
}

// Kinds is the whole taxonomy, in the order the model sees it.
var Kinds = []Kind{
	{ID: "concept", Label: "概念", Hint: "术语、定义、原理、机制、规则、分类"},
	{ID: "fact", Label: "事实", Hint: "可核对的陈述与结论、时间地点人物、结论性数据"},
	{ID: "procedure", Label: "步骤", Hint: "有先后顺序的做法、流程、操作方法"},
	{ID: "parameter", Label: "参数", Hint: "数值、阈值、配置、规格、限制、计量单位"},
	{ID: "tip", Label: "技巧", Hint: "经验、注意事项、易错点、优化与替代做法"},
	{ID: "entity", Label: "对象", Hint: "人物、组织、产品、作品、地点及其属性"},
	{ID: "faq", Label: "问答", Hint: "原文中已有的一问一答"},
}

// AllowsKind reports whether kind is in the built-in taxonomy.
func AllowsKind(kind string) bool {
	for _, allowed := range Kinds {
		if allowed.ID == kind {
			return true
		}
	}
	return false
}

// MaxKindRunes caps an operator-typed kind. Built-in ids are short ASCII
// words; a custom one is whatever the owner types, so the cap is on runes.
const MaxKindRunes = 24

// NormalizeKind trims and lowercases a kind so " Snack " and "snack" land on
// the same value instead of becoming two shelves.
func NormalizeKind(kind string) string {
	return strings.ToLower(strings.TrimSpace(kind))
}

// ValidKind reports whether an operator may store this kind: non-empty, within
// the rune cap, and free of control characters. Built-in ids pass.
func ValidKind(kind string) bool {
	if kind == "" || utf8.RuneCountInString(kind) > MaxKindRunes {
		return false
	}
	for _, r := range kind {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// KindIDs lists the taxonomy ids in display order.
func KindIDs() []string {
	ids := make([]string, 0, len(Kinds))
	for _, kind := range Kinds {
		ids = append(ids, kind.ID)
	}
	return ids
}

// MaxTags caps the free-form tags on one knowledge item.
const MaxTags = 8

type CandidateDraft struct {
	Kind          string
	CanonicalKey  string
	Title         string
	Body          string
	Tags          []string
	Metadata      json.RawMessage
	EvidenceQuote string
	Confidence    float64
}

// KnowledgeDraft is one validated extraction result ready to be stored. Its
// evidence is inline on the knowledge row: the quote and its recomputed rune
// offsets into the material's stored text, so there is no evidence table and
// no chunk to point at.
type KnowledgeDraft struct {
	Kind          string
	Title         string
	Body          string
	Tags          []string
	Metadata      json.RawMessage
	Fingerprint   string
	EvidenceQuote string
	QuoteStart    int
	QuoteEnd      int
}

type AnswerDraft struct {
	Answer      string  `json:"answer"`
	CitationIDs []int64 `json:"citation_ids"`
}

type Evidence struct {
	DocumentID   int64  `json:"document_id"`
	DocumentName string `json:"document_name"`
	// Topic is the topic of the source material this quote came from, so a
	// caller can tell which shelf a piece of evidence was taken off.
	Topic           string `json:"topic"`
	Quote           string `json:"quote"`
	QuoteStart      int    `json:"quote_start"`
	QuoteEnd        int    `json:"quote_end"`
	DocumentVersion int    `json:"document_version"`
	// Superseded marks evidence from a document that a newer processed
	// version of the same source has replaced. It stays visible for
	// provenance but is never sent to the model as current fact.
	Superseded bool `json:"superseded"`
}

type KnowledgeItem struct {
	ID int64 `json:"id"`
	// DocumentID is the single material this item was extracted from. The
	// library does not merge the same knowledge from two materials: identical
	// content is reported as a duplicate material instead.
	DocumentID int64           `json:"document_id"`
	Kind       string          `json:"kind"`
	Title      string          `json:"title"`
	Body       string          `json:"body"`
	Tags       []string        `json:"tags"`
	Metadata   json.RawMessage `json:"metadata"`
	CreatedAt  string          `json:"created_at"`
	UpdatedAt  string          `json:"updated_at"`
	Evidence   []Evidence      `json:"evidence"`
	// Topics is the topic of the one material this item came from, omitted
	// when the material is unfiled.
	Topics []string `json:"topics"`
	// UseCount is how many drafts were written from this item, so the
	// knowledge base shows what has not been written about yet.
	UseCount int `json:"use_count"`
	// ReadCount is how many times the owner has opened this item. Reading is
	// the point of this library, so the count is surfaced next to each item.
	ReadCount int `json:"read_count"`
	// Match and Score are set on retrieval results: how the item was found
	// (keyword, semantic, or both) and its semantic cosine similarity.
	Match string  `json:"match,omitempty"`
	Score float64 `json:"score,omitempty"`
}

type Citation struct {
	KnowledgeID int64      `json:"knowledge_id"`
	Title       string     `json:"title"`
	Evidence    []Evidence `json:"evidence"`
}

type Answer struct {
	RunID     string     `json:"run_id"`
	Text      string     `json:"answer"`
	Citations []Citation `json:"citations"`
}

// SourceDocument is one material's stored text, returned to a caller that
// followed a citation back to its origin.
type SourceDocument struct {
	DocumentID   int64  `json:"document_id"`
	DocumentName string `json:"document_name"`
	Topic        string `json:"topic"`
	Text         string `json:"text"`
}
