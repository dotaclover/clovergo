package domain

// PromptOverride is the owner's replacement for one built-in prompt layer.
// The built-in text stays in internal/prompts/layers; a row here simply means
// "use this instead", and deleting it restores the built-in text.
type PromptOverride struct {
	Layer     string `json:"layer"`
	Text      string `json:"text"`
	Revision  int    `json:"revision"`
	UpdatedAt string `json:"updated_at"`
}
