package domain

const (
	LanguageEnglish  = "en"
	LanguageJapanese = "ja"
	LanguageChinese  = "zh"
	LanguageUnknown  = "und"

	// DocumentReady means the material was summarized and stored. It remains
	// archived until the owner explicitly requests knowledge extraction.
	DocumentReady = "ready"
	// DocumentProcessing means a worker claimed the document.
	DocumentProcessing = "processing"
	// DocumentPublished means extraction finished and at least one
	// knowledge item was published.
	DocumentPublished = "published"
	// DocumentProcessedEmpty means extraction finished cleanly but the
	// material carried no reusable knowledge.
	DocumentProcessedEmpty = "processed_empty"
	// DocumentAnalysisFailed is terminal: either the extraction failed, or
	// the document exhausted MaxDocumentAttempts.
	DocumentAnalysisFailed = "analysis_failed"
)

// MaxDocumentAttempts bounds automatic retries of one document so a material
// that always fails cannot occupy the queue forever. A person can always
// re-queue it by hand from the materials page.
const MaxDocumentAttempts = 3

// Document is one imported source file version. Re-importing a file with the
// same name and different content creates the next version of its source_key.
type Document struct {
	ID        int64  `json:"id"`
	Filename  string `json:"filename"`
	SourceKey string `json:"source_key"`
	Version   int    `json:"version"`
	SHA256    string `json:"sha256"`
	MediaType string `json:"media_type"`
	Language  string `json:"language"`
	// Topic is the human-named shelf this material lives on (日语, 英语, PHP,
	// F1...). It is deliberately separate from Language: a programming
	// language or a sport is a topic but not a language, and a material with
	// no topic is filed under "" rather than guessed at.
	Topic string `json:"topic"`
	// Summary is the model's short summary of the whole material, written at
	// upload time. It is what the uploader sees while a slow extraction runs.
	Summary      string `json:"summary,omitempty"`
	StoredPath   string `json:"-"`
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message,omitempty"`
	Attempts     int    `json:"attempts"`
	UsefulCount  int    `json:"useful_count"`
	// UseCount is how many drafts were written from this material's original
	// text.
	UseCount        int     `json:"use_count"`
	ProcessingRunID string  `json:"processing_run_id,omitempty"`
	CreatedAt       string  `json:"created_at"`
	ProcessedAt     *string `json:"processed_at,omitempty"`
	// RootID/RelPath record the registered folder and the slash-separated
	// relative path this version was imported from (RootID 0 = uploaded or
	// pasted). Missing means the last scan no longer found the file on disk.
	// OriginSize/OriginMTime let a scan skip unchanged files without reading
	// them.
	RootID      int64  `json:"root_id"`
	RelPath     string `json:"rel_path,omitempty"`
	Missing     bool   `json:"missing"`
	OriginSize  int64  `json:"-"`
	OriginMTime int64  `json:"-"`
}

// LibraryRoot is a folder the owner registered for scanning. Deleting one only
// forgets the folder; imported materials stay.
type LibraryRoot struct {
	ID            int64  `json:"id"`
	Path          string `json:"path"`
	Name          string `json:"name"`
	CreatedAt     string `json:"created_at"`
	DocumentCount int    `json:"document_count"`
}
