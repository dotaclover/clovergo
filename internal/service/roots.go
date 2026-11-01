package service

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"clovergo/internal/domain"
	"clovergo/internal/extractor"
)

// maxScanFiles bounds one scan so that pointing a root at a drive does not
// walk it forever; maxScanEntries bounds every entry visited (folders and
// unsupported files included), so a tree of non-material files cannot either.
const (
	maxScanFiles   = 5000
	maxScanEntries = 50000
)

// ScanEntry is one file a scan reports on. Reason is a stable code for skipped
// files (too_large, empty, too_long, invalid_utf8, invalid_docx, unreadable).
type ScanEntry struct {
	RelPath    string `json:"rel_path"`
	Size       int64  `json:"size"`
	Reason     string `json:"reason,omitempty"`
	DocumentID int64  `json:"document_id,omitempty"`
}

// ScanReport is what a scan of one root found. Nothing is imported by a scan
// and no model is called: the owner reads the report, then imports files.
type ScanReport struct {
	Root      domain.LibraryRoot `json:"root"`
	New       []ScanEntry        `json:"new"`
	Changed   []ScanEntry        `json:"changed"`
	Skipped   []ScanEntry        `json:"skipped"`
	Missing   []ScanEntry        `json:"missing"`
	Unchanged int                `json:"unchanged"`
	// Known is always 0: identical bytes under another path are a separate
	// material now (sha256 is no longer globally unique).
	Known     int  `json:"known"`
	Other     int  `json:"other"`
	Truncated bool `json:"truncated"`
	// ErrorDirs lists folders (relative, "/"-separated; "." is the root) that
	// could not be read. Materials under them are not marked missing.
	ErrorDirs []string `json:"error_dirs"`
}

// AddRoot registers a folder of the owner's own files. Only the owner can do
// this (through the API); the model never receives a path.
func (s *Service) AddRoot(ctx context.Context, path, name string) (*domain.LibraryRoot, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, appError(CodeInvalidInput, "path is required", nil)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, appError(CodeInvalidInput, "path is invalid", err)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(abs))
	if err != nil {
		return nil, appError(CodeInvalidInput, "folder does not exist", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return nil, appError(CodeInvalidInput, "path must be an existing folder", err)
	}
	existing, err := s.repo.ListRoots(ctx)
	if err != nil {
		return nil, appError(CodeInternal, "list folders failed", err)
	}
	for _, root := range existing {
		if root.Path == resolved || (runtime.GOOS == "windows" && strings.EqualFold(root.Path, resolved)) {
			return nil, appError(CodeInvalidInput, "folder is already registered", nil)
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = filepath.Base(resolved)
	}
	root, err := s.repo.AddRoot(ctx, resolved, name)
	if err != nil {
		return nil, appError(CodeInternal, "save folder failed", err)
	}
	s.log.InfoContext(ctx, "library root added", "root_id", root.ID)
	return root, nil
}

func (s *Service) ListRoots(ctx context.Context) ([]domain.LibraryRoot, error) {
	roots, err := s.repo.ListRoots(ctx)
	if err != nil {
		return nil, appError(CodeInternal, "list folders failed", err)
	}
	return roots, nil
}

// DeleteRoot forgets a folder; its imported materials stay in the library.
func (s *Service) DeleteRoot(ctx context.Context, id int64) error {
	if id <= 0 {
		return appError(CodeInvalidInput, "folder id must be positive", nil)
	}
	if err := s.repo.DeleteRoot(ctx, id); err != nil {
		return mapRepositoryError(err, CodeRootNotFound)
	}
	return nil
}

func (s *Service) getRoot(ctx context.Context, id int64) (*domain.LibraryRoot, error) {
	if id <= 0 {
		return nil, appError(CodeInvalidInput, "folder id must be positive", nil)
	}
	root, err := s.repo.GetRoot(ctx, id)
	if err != nil {
		return nil, mapRepositoryError(err, CodeRootNotFound)
	}
	return root, nil
}

func skipReason(err error) string {
	switch {
	case errors.Is(err, extractor.ErrTooLong):
		return "too_long"
	case errors.Is(err, extractor.ErrEmptyDocument):
		return "empty"
	case errors.Is(err, extractor.ErrInvalidUTF8):
		return "invalid_utf8"
	case errors.Is(err, extractor.ErrInvalidDocx):
		return "invalid_docx"
	default:
		return "unreadable"
	}
}

// ScanRoot walks a registered folder and classifies its supported files against
// what the library already holds. It reads files but never changes them, never
// follows symbolic links, and never calls a model.
func (s *Service) ScanRoot(ctx context.Context, id int64) (*ScanReport, error) {
	root, err := s.getRoot(ctx, id)
	if err != nil {
		return nil, err
	}
	if info, statErr := os.Stat(root.Path); statErr != nil || !info.IsDir() {
		return nil, appError(CodeInvalidState, "folder is no longer available on disk", statErr)
	}
	known, err := s.repo.ListRootDocuments(ctx, root.ID)
	if err != nil {
		return nil, appError(CodeInternal, "list folder materials failed", err)
	}
	latest := make(map[string]domain.Document)
	for _, document := range known {
		if prev, ok := latest[document.RelPath]; !ok || document.Version > prev.Version {
			latest[document.RelPath] = document
		}
	}

	report := &ScanReport{
		Root: *root, New: []ScanEntry{}, Changed: []ScanEntry{}, Skipped: []ScanEntry{}, Missing: []ScanEntry{},
		ErrorDirs: []string{},
	}
	seen := make(map[string]struct{})
	visited, entries := 0, 0
	relOf := func(path string) string {
		relNative, err := filepath.Rel(root.Path, path)
		if err != nil {
			return ""
		}
		return filepath.ToSlash(relNative)
	}

	walkErr := filepath.WalkDir(root.Path, func(path string, entry fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		entries++
		if entries > maxScanEntries {
			report.Truncated = true
			return filepath.SkipAll
		}
		if err != nil {
			if entry == nil || entry.IsDir() {
				if rel := relOf(path); rel != "" {
					report.ErrorDirs = append(report.ErrorDirs, rel)
				}
				if entry == nil {
					return nil
				}
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if path != root.Path && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~$") || name == "node_modules") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if !extractor.Supported(name) {
			report.Other++
			return nil
		}
		visited++
		if visited > maxScanFiles {
			report.Truncated = true
			return filepath.SkipAll
		}
		relNative, err := filepath.Rel(root.Path, path)
		if err != nil {
			return nil
		}
		rel := filepath.ToSlash(relNative)
		seen[rel] = struct{}{}
		info, err := entry.Info()
		if err != nil {
			report.Skipped = append(report.Skipped, ScanEntry{RelPath: rel, Reason: "unreadable"})
			return nil
		}
		size, mtime := info.Size(), info.ModTime().UnixNano()
		prev, hasPrev := latest[rel]
		if hasPrev && prev.OriginSize == size && prev.OriginMTime == mtime {
			report.Unchanged++
			return nil
		}
		if size == 0 {
			report.Skipped = append(report.Skipped, ScanEntry{RelPath: rel, Size: size, Reason: "empty"})
			return nil
		}
		if size > s.maxUploadBytes {
			report.Skipped = append(report.Skipped, ScanEntry{RelPath: rel, Size: size, Reason: "too_large"})
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			report.Skipped = append(report.Skipped, ScanEntry{RelPath: rel, Size: size, Reason: "unreadable"})
			return nil
		}
		prepared, err := prepareMaterial(name, data, s.maxDocumentRunes)
		if err != nil {
			report.Skipped = append(report.Skipped, ScanEntry{RelPath: rel, Size: size, Reason: skipReason(err)})
			return nil
		}
		if hasPrev && prev.SHA256 == prepared.Hash {
			report.Unchanged++
			return nil
		}
		entryOut := ScanEntry{RelPath: rel, Size: size}
		if hasPrev {
			entryOut.DocumentID = prev.ID
			report.Changed = append(report.Changed, entryOut)
		} else {
			report.New = append(report.New, entryOut)
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, filepath.SkipAll) {
		return nil, appError(CodeInternal, "scan folder failed", walkErr)
	}

	if !report.Truncated {
		underErrorDir := func(rel string) bool {
			for _, dir := range report.ErrorDirs {
				if dir == "." || rel == dir || strings.HasPrefix(rel, dir+"/") {
					return true
				}
			}
			return false
		}
		missing := make([]string, 0)
		for rel, document := range latest {
			if _, ok := seen[rel]; !ok && !underErrorDir(rel) {
				missing = append(missing, rel)
				report.Missing = append(report.Missing, ScanEntry{RelPath: rel, DocumentID: document.ID})
			}
		}
		if err := s.repo.MarkRootMissing(ctx, root.ID, missing); err != nil {
			return nil, appError(CodeInternal, "record missing files failed", err)
		}
	}
	s.log.InfoContext(ctx, "library root scanned", "root_id", root.ID, "new", len(report.New), "changed", len(report.Changed),
		"skipped", len(report.Skipped), "missing", len(report.Missing), "unchanged", report.Unchanged)
	return report, nil
}

// ImportRootFile imports one file of a registered folder: a read, a conversion
// and one summary call, exactly like an upload. The relative path must stay
// inside the folder and must not pass through a symbolic link.
func (s *Service) ImportRootFile(ctx context.Context, rootID int64, rel, topic string) (MaterialImportResult, error) {
	root, err := s.getRoot(ctx, rootID)
	if err != nil {
		return MaterialImportResult{}, err
	}
	native := filepath.FromSlash(strings.TrimSpace(rel))
	if native == "" || !filepath.IsLocal(native) {
		return MaterialImportResult{}, appError(CodeInvalidInput, "path must be relative to the folder", nil)
	}
	for _, part := range strings.Split(native, string(filepath.Separator)) {
		if strings.HasPrefix(part, ".") || strings.HasPrefix(part, "~$") || part == "node_modules" {
			return MaterialImportResult{}, appError(CodeInvalidInput, "path is excluded from folder scans", nil)
		}
	}
	full := filepath.Join(root.Path, native)
	info, err := os.Lstat(full)
	if err != nil {
		return MaterialImportResult{}, appError(CodeSourceNotFound, "file not found in folder", err)
	}
	if !info.Mode().IsRegular() {
		return MaterialImportResult{}, appError(CodeInvalidInput, "path must be a regular file", nil)
	}
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		return MaterialImportResult{}, appError(CodeSourceNotFound, "file not found in folder", err)
	}
	if back, relErr := filepath.Rel(root.Path, resolved); relErr != nil || !filepath.IsLocal(back) {
		return MaterialImportResult{}, appError(CodeInvalidInput, "path must stay inside the folder", relErr)
	}
	if info.Size() > s.maxUploadBytes {
		return MaterialImportResult{}, appError(CodeFileTooLarge, "document exceeds configured upload limit", nil)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return MaterialImportResult{}, appError(CodeInternal, "read file failed", err)
	}
	imported, err := s.storeMaterial(ctx, filepath.Base(native), data, topic, materialOrigin{
		RootID: root.ID, RelPath: filepath.ToSlash(native), Size: info.Size(), MTime: info.ModTime().UnixNano(),
	})
	if err != nil {
		return MaterialImportResult{}, err
	}
	return MaterialImportResult{Document: imported.Document, Duplicate: imported.Duplicate}, nil
}
