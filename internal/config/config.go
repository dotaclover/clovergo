package config

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	BaseDir        string
	Address        string
	DatabasePath   string
	LibraryDir     string
	MaxUploadBytes int64
	// MaxDocumentRunes bounds how much source text one material may hold. A
	// material is sent to the model whole in a single extraction call, so this
	// is the one bound that keeps an upload inside the model's context window.
	// Raise it together with ARK_TIMEOUT_SECONDS.
	MaxDocumentRunes int
	// MinExtractConfidence (env AUTO_PUBLISH_MIN_CONFIDENCE, a historical
	// name) is the extraction confidence below which an extracted knowledge
	// draft is discarded instead of stored. Nothing is published to WeChat by it.
	MinExtractConfidence float64
	ArkBaseURL           string
	ArkAPIKey            string
	ArkModel             string
	ArkTimeout           time.Duration
	// ArkEmbeddingModel enables semantic search when set; empty keeps search
	// keyword-only, which /health/ready reports.
	ArkEmbeddingModel      string
	ArkEmbeddingDimensions int
	// WeChat Official Account credentials for pushing drafts into its draft
	// box. Optional; without them the push button reports it is unconfigured.
	WechatAppID        string
	WechatAppSecret    string
	WechatAuthor       string
	WechatThumbMediaID string
	WechatProxyURL     string
}

func Load() (Config, error) {
	baseDir, err := baseDirectory()
	if err != nil {
		return Config{}, err
	}
	if err := godotenv.Load(filepath.Join(baseDir, ".env")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Config{}, fmt.Errorf("read .env: %w", err)
	}

	maxUploadMB, err := positiveInt("MAX_UPLOAD_MB", 10)
	if err != nil {
		return Config{}, err
	}
	timeoutSeconds, err := positiveInt("ARK_TIMEOUT_SECONDS", 240)
	if err != nil {
		return Config{}, err
	}
	maxDocumentRunes, err := positiveInt("MAX_DOCUMENT_RUNES", 200000)
	if err != nil {
		return Config{}, err
	}
	if maxDocumentRunes < 1000 || maxDocumentRunes > 1000000 {
		return Config{}, fmt.Errorf("MAX_DOCUMENT_RUNES must be between 1000 and 1000000")
	}
	embeddingDimensions, err := positiveInt("ARK_EMBEDDING_DIMENSIONS", 1024)
	if err != nil {
		return Config{}, err
	}
	if embeddingDimensions < 64 || embeddingDimensions > 4096 {
		return Config{}, fmt.Errorf("ARK_EMBEDDING_DIMENSIONS must be between 64 and 4096")
	}
	minConfidence, err := boundedFloat("AUTO_PUBLISH_MIN_CONFIDENCE", 0.72, 0.01, 1)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		BaseDir:                baseDir,
		Address:                env("APP_ADDR", "127.0.0.1:8088"),
		DatabasePath:           resolvePath(baseDir, env("DATABASE_PATH", "data/clovergo.db")),
		LibraryDir:             resolvePath(baseDir, env("LIBRARY_DIR", "data/library")),
		MaxUploadBytes:         int64(maxUploadMB) * 1024 * 1024,
		MaxDocumentRunes:       maxDocumentRunes,
		MinExtractConfidence:   minConfidence,
		ArkBaseURL:             strings.TrimRight(env("ARK_BASE_URL", "https://ark.cn-beijing.volces.com/api/v3"), "/"),
		ArkAPIKey:              strings.TrimSpace(os.Getenv("ARK_API_KEY")),
		ArkModel:               env("ARK_MODEL", "deepseek-v4-1-flash-260910"),
		ArkTimeout:             time.Duration(timeoutSeconds) * time.Second,
		ArkEmbeddingModel:      strings.TrimSpace(os.Getenv("ARK_EMBEDDING_MODEL")),
		ArkEmbeddingDimensions: embeddingDimensions,
		WechatAppID:            strings.TrimSpace(os.Getenv("WECHAT_APP_ID")),
		WechatAppSecret:        strings.TrimSpace(os.Getenv("WECHAT_APP_SECRET")),
		WechatAuthor:           strings.TrimSpace(os.Getenv("WECHAT_AUTHOR")),
		WechatThumbMediaID:     strings.TrimSpace(os.Getenv("WECHAT_THUMB_MEDIA_ID")),
		WechatProxyURL:         strings.TrimSpace(os.Getenv("WECHAT_PROXY_URL")),
	}
	if strings.TrimSpace(cfg.Address) == "" || strings.TrimSpace(cfg.DatabasePath) == "" || strings.TrimSpace(cfg.LibraryDir) == "" {
		return Config{}, fmt.Errorf("APP_ADDR, DATABASE_PATH, and LIBRARY_DIR must not be empty")
	}
	host, port, err := net.SplitHostPort(cfg.Address)
	if err != nil {
		return Config{}, fmt.Errorf("APP_ADDR must be a loopback host:port")
	}
	if strings.EqualFold(host, "localhost") {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	portNumber, portErr := strconv.Atoi(port)
	if ip == nil || !ip.IsLoopback() || portErr != nil || portNumber < 1 || portNumber > 65535 {
		return Config{}, fmt.Errorf("APP_ADDR must use a loopback IP and a port between 1 and 65535")
	}
	cfg.Address = net.JoinHostPort(host, port)
	return cfg, nil
}

// baseDirectory is the installed executable's directory when a .env sits next
// to it (the install.ps1 layout), so shortcuts and other working directories
// still find the right database. Otherwise, such as with go run, it is the
// current working directory.
func baseDirectory() (string, error) {
	if executable, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
		dir := filepath.Dir(executable)
		if info, err := os.Stat(filepath.Join(dir, ".env")); err == nil && !info.IsDir() {
			return dir, nil
		}
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	return dir, nil
}

func resolvePath(baseDir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(baseDir, path)
}

func boundedFloat(name string, fallback, min, max float64) (float64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < min || value > max {
		return 0, fmt.Errorf("%s must be a number between %.2f and %.2f", name, min, max)
	}
	return value, nil
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func positiveInt(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}
