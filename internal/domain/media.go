package domain

// Draft statuses. A draft row exists before the model call, so the list can
// show it while it is being written.
const (
	MediaDraftRunning   = "running"
	MediaDraftGenerated = "generated"
	MediaDraftFailed    = "failed"
)

// MediaDraft is one platform-specific self-media draft written from confirmed
// knowledge. Article or Gallery is set, matching Format. Drafts are text only:
// images are described by prompts, and publishing is always done by a person.
type MediaDraft struct {
	ID            int64           `json:"id"`
	RunID         string          `json:"run_id"`
	ArtifactID    int64           `json:"artifact_id"`
	Platform      string          `json:"platform"`
	Format        string          `json:"format"`
	Topic         string          `json:"topic"`
	Angle         string          `json:"angle"`
	Audience      string          `json:"audience"`
	Article       *ArticleContent `json:"article,omitempty"`
	Gallery       *GalleryContent `json:"gallery,omitempty"`
	Model         string          `json:"model"`
	PromptVersion string          `json:"prompt_version"`
	// Status is running while the background worker writes the article,
	// generated when it is done, and failed when the model call or its checks
	// failed. ErrorMessage says why a failed draft has no article.
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message"`
	CreatedAt    string `json:"created_at"`
	// WechatMediaID is set once the draft was pushed to the Official
	// Account's draft box. WechatPublishID/WechatPublishedAt are set only when
	// the owner chose to submit it for publishing as well.
	WechatMediaID     string  `json:"wechat_media_id"`
	WechatPushedAt    *string `json:"wechat_pushed_at,omitempty"`
	WechatPublishID   string  `json:"wechat_publish_id"`
	WechatPublishedAt *string `json:"wechat_published_at,omitempty"`
}

// MediaDraftBody is the generated part of a draft. The sources a draft was
// written from stay on the server as its input; they are not cited in the
// output, because a local knowledge item cannot be referenced from the
// published article.
type MediaDraftBody struct {
	Article *ArticleContent `json:"article,omitempty"`
	Gallery *GalleryContent `json:"gallery,omitempty"`
}

// ArticleContent is a WeChat Official Account long-form article. It has the
// shape an article has: a lead that opens the piece, chapters that develop it,
// and a conclusion that closes it. This is deliberately not a note or a
// checklist — those are the shapes of a feed post, and copying them produces a
// listicle where a reader expected an article.
//
// Body text is plain text, not Markdown, because the author pastes it into the
// WeChat editor.
type ArticleContent struct {
	Title            string           `json:"title"`
	Summary          string           `json:"summary"`
	Lead             string           `json:"lead"`
	CoverImagePrompt string           `json:"cover_image_prompt"`
	Sections         []ArticleSection `json:"sections"`
	Conclusion       string           `json:"conclusion"`
	Tags             []string         `json:"tags"`
}

// ArticleSection is one chapter: a heading and the prose under it.
type ArticleSection struct {
	Order   int    `json:"order"`
	Heading string `json:"heading"`
	Body    string `json:"body"`
	// ImagePrompt and ImageCaption are both optional and come as a pair: the
	// article uses images sparingly, and when one appears it gets a caption.
	ImagePrompt  string `json:"image_prompt,omitempty"`
	ImageCaption string `json:"image_caption,omitempty"`
}

// GalleryContent is a WeChat picture post ("小绿书"): one short caption under a
// set of images. Each image is a prompt, not a picture. At push time a slot
// without an upload becomes a local card showing only the title; no image
// model is involved and the prompt is not printed.
type GalleryContent struct {
	Title   string         `json:"title"`
	Caption string         `json:"caption"`
	Images  []GalleryImage `json:"images"`
}

// GalleryImage is one picture of a picture post.
type GalleryImage struct {
	Order  int    `json:"order"`
	Prompt string `json:"prompt"`
}

// MediaDraftInput is what the creator asks for; it is untrusted text.
type MediaDraftInput struct {
	Format   string `json:"format"`
	Topic    string `json:"topic"`
	Angle    string `json:"angle"`
	Audience string `json:"audience"`
	// ImageCount is the number of pictures asked of a gallery post; 0 means
	// the format's default. Articles ignore it.
	ImageCount int `json:"image_count"`
}
