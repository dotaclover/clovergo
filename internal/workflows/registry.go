package workflows

import (
	"errors"
	"sort"
	"strings"
)

const (
	KnowledgeIngestKey     = "knowledge.ingest"
	KnowledgeAnswerKey     = "knowledge.answer"
	MediaDraftKey          = "media.draft"
	AgentChatKey           = "agent.chat"
	InitialWorkflowVersion = "1.0.0"
)

var ErrUnknownWorkflow = errors.New("unknown workflow")

// Definition is a trusted, compile-time workflow identity.
type Definition struct {
	Key     string `json:"key"`
	Version string `json:"version"`
}

var registry = map[string]Definition{
	workflowRef(KnowledgeIngestKey, InitialWorkflowVersion): {
		Key: KnowledgeIngestKey, Version: InitialWorkflowVersion,
	},
	workflowRef(KnowledgeAnswerKey, InitialWorkflowVersion): {
		Key: KnowledgeAnswerKey, Version: InitialWorkflowVersion,
	},
	workflowRef(MediaDraftKey, InitialWorkflowVersion): {
		Key: MediaDraftKey, Version: InitialWorkflowVersion,
	},
	workflowRef(AgentChatKey, InitialWorkflowVersion): {
		Key: AgentChatKey, Version: InitialWorkflowVersion,
	},
}

func Get(key, version string) (Definition, error) {
	definition, ok := registry[workflowRef(key, version)]
	if !ok {
		return Definition{}, ErrUnknownWorkflow
	}
	return definition, nil
}

func GetLatest(key string) (Definition, error) {
	return Get(key, InitialWorkflowVersion)
}

func List() []Definition {
	items := make([]Definition, 0, len(registry))
	for _, definition := range registry {
		items = append(items, definition)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Key == items[j].Key {
			return items[i].Version < items[j].Version
		}
		return items[i].Key < items[j].Key
	})
	return items
}

func workflowRef(key, version string) string {
	return strings.ToLower(strings.TrimSpace(key)) + "/" + strings.TrimSpace(version)
}
