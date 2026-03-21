-- Drop the outbox table — webhook delivery is now handled by
-- the WebhookProducer/WebhookWorker processes via S3-backed queue.
DROP TABLE IF EXISTS outbox CASCADE;
