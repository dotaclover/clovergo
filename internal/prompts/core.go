package prompts

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"strings"
)

// Core prompt text lives in Markdown files embedded at build time. Editing a
// file and rebuilding is a code change like any other; CoreVersion derives from
// the file contents, so runs and drafts always record which text produced them
// without anyone remembering to bump a constant.
//
//go:embed layers/*.md
var layerFiles embed.FS

type layerDef struct {
	key  string
	name string
	file string
}

// layerDefs is the edit surface shown in settings, in composition order. Only
// prose lives here: the taxonomy and the numeric format rules are generated in
// code and cannot be replaced, because validation reads the same tables.
var layerDefs = []layerDef{
	{key: "extraction", name: "资料提炼", file: "layers/extraction.md"},
	{key: "summary", name: "资料摘要", file: "layers/summary.md"},
	{key: "answer", name: "带引用问答", file: "layers/answer.md"},
	{key: "media", name: "写作核心规则", file: "layers/media.md"},
	{key: "gallery", name: "图文帖规则", file: "layers/gallery.md"},
	{key: "persona", name: "创作者人设", file: "layers/persona.md"},
	{key: "agent", name: "Agent 规则", file: "layers/agent.md"},
}

// LayerDefinition is one editable layer with the text compiled into the binary.
type LayerDefinition struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Text string `json:"text"`
}

var (
	layerTexts = map[string]string{}
	layerNames = map[string]string{}
	// CoreVersion identifies the embedded prompt text. It is the first eight
	// hex digits of a hash over every layer file, so any content change in a
	// rebuild changes it.
	CoreVersion string
)

func init() {
	sum := sha256.New()
	for _, def := range layerDefs {
		content, err := layerFiles.ReadFile(def.file)
		if err != nil {
			panic(fmt.Sprintf("prompt layer %s is missing: %v", def.file, err))
		}
		text := strings.TrimSpace(string(content))
		layerTexts[def.key] = text
		layerNames[def.key] = def.name
		fmt.Fprintf(sum, "%s\n%s\n", def.key, text)
	}
	CoreVersion = "core-" + hex.EncodeToString(sum.Sum(nil))[:8]
}

// coreText returns the built-in text of a layer. An unknown key is a bug, so
// it fails loudly instead of silently composing an empty prompt.
func coreText(key string) string {
	text, ok := layerTexts[key]
	if !ok {
		panic("unknown prompt layer: " + key)
	}
	return text
}

func coreName(key string) string {
	return layerNames[key]
}

// Layers lists the editable layers and their built-in text.
func Layers() []LayerDefinition {
	result := make([]LayerDefinition, 0, len(layerDefs))
	for _, def := range layerDefs {
		result = append(result, LayerDefinition{Key: def.key, Name: def.name, Text: layerTexts[def.key]})
	}
	return result
}

// IsLayer reports whether key names an editable layer.
func IsLayer(key string) bool {
	_, ok := layerTexts[key]
	return ok
}
