package extractor

import (
	"errors"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

var (
	ErrUnsupportedFormat = errors.New("unsupported document format")
	ErrInvalidUTF8       = errors.New("document is not valid UTF-8")
	ErrEmptyDocument     = errors.New("document contains no text")
	ErrTooLong           = errors.New("document exceeds the maximum length")
	ErrInvalidDocx       = errors.New("file is not a readable .docx document")
)

// defaultMaxDocumentRunes is used when the caller supplies no bound. Extraction
// sends the whole material to the model in one call, so this is the single
// bound that keeps a runaway upload inside the model's context window.
const defaultMaxDocumentRunes = 200000

// ExtractText validates one material and returns its whole normalized text.
//
// A material is delivered to the model as a single string, never in slices: the
// configured model has a context window large enough to read a whole material,
// and a single call means each passage is read once and produces one set of
// knowledge instead of near-duplicates at slice boundaries.
func ExtractText(filename string, data []byte, maxRunes int) (mediaType, text string, err error) {
	if maxRunes <= 0 {
		maxRunes = defaultMaxDocumentRunes
	}
	mediaType = mediaTypeFor(filename)
	if mediaType == "" {
		return "", "", ErrUnsupportedFormat
	}
	if Converted(filename) {
		converted, convErr := docxToText(data)
		if convErr != nil {
			return "", "", convErr
		}
		data = []byte(converted)
	}
	text, err = NormalizeText(data)
	if err != nil {
		return "", "", err
	}
	if utf8.RuneCountInString(text) > maxRunes {
		return "", "", ErrTooLong
	}
	return mediaType, text, nil
}

// NormalizeText turns stored bytes into the exact text that extraction reads
// and that evidence offsets are computed against. It is deterministic on
// purpose: reprocessing reads the stored file and re-normalizes it, and a
// stored quote with its rune offsets has to land on the same slice.
func NormalizeText(data []byte) (string, error) {
	if !utf8.Valid(data) {
		return "", ErrInvalidUTF8
	}
	text := strings.TrimPrefix(string(data), "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if strings.TrimSpace(text) == "" {
		return "", ErrEmptyDocument
	}
	return text, nil
}

func mediaTypeFor(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".md", ".markdown":
		return "text/markdown"
	case ".txt":
		return "text/plain"
	case ".docx":
		return mediaTypeDocx
	default:
		return ""
	}
}

const mediaTypeDocx = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

// Supported reports whether a file name has an importable extension.
func Supported(filename string) bool { return mediaTypeFor(filename) != "" }

// Converted reports whether the format is converted to plain text on import.
// Such a material is stored as its converted text (as .txt), not as the
// original bytes, so the stored file always is the text that evidence offsets
// are computed against.
func Converted(filename string) bool { return mediaTypeFor(filename) == mediaTypeDocx }
