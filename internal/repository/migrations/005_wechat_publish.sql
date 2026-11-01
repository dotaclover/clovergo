-- A draft pushed with "publish" also records the publish_id WeChat returned
-- from freepublish/submit and when it was submitted.
ALTER TABLE media_drafts ADD COLUMN wechat_publish_id TEXT NOT NULL DEFAULT '';
ALTER TABLE media_drafts ADD COLUMN wechat_published_at TEXT;
