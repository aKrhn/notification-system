CREATE OR REPLACE FUNCTION trigger_set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE notifications (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id            UUID,
    idempotency_key     VARCHAR(255) UNIQUE,
    recipient           VARCHAR(255) NOT NULL,
    channel             VARCHAR(20)  NOT NULL,
    content             TEXT         NOT NULL,
    subject             VARCHAR(255),
    priority            VARCHAR(10)  NOT NULL DEFAULT 'normal',
    status              VARCHAR(20)  NOT NULL DEFAULT 'pending',
    provider_message_id VARCHAR(255),
    retry_count         INT          NOT NULL DEFAULT 0,
    max_retries         INT          NOT NULL DEFAULT 3,
    next_retry_at       TIMESTAMPTZ,
    scheduled_at        TIMESTAMPTZ,
    sent_at             TIMESTAMPTZ,
    failed_at           TIMESTAMPTZ,
    error_message       TEXT,
    metadata            JSONB,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_channel  CHECK (channel  IN ('sms', 'email', 'push')),
    CONSTRAINT chk_status   CHECK (status   IN ('pending', 'queued', 'processing', 'sent', 'delivered', 'failed', 'cancelled')),
    CONSTRAINT chk_priority CHECK (priority IN ('high', 'normal', 'low'))
);

CREATE INDEX idx_notifications_batch_id   ON notifications (batch_id) WHERE batch_id IS NOT NULL;
CREATE INDEX idx_notifications_status     ON notifications (status);
CREATE INDEX idx_notifications_channel    ON notifications (channel);
CREATE INDEX idx_notifications_created_at ON notifications (created_at);
CREATE INDEX idx_notifications_cursor     ON notifications (created_at DESC, id DESC);
CREATE INDEX idx_notifications_scheduled  ON notifications (scheduled_at) WHERE status = 'pending' AND scheduled_at IS NOT NULL;
CREATE INDEX idx_notifications_retry      ON notifications (next_retry_at) WHERE next_retry_at IS NOT NULL;

CREATE TRIGGER set_updated_at
    BEFORE UPDATE ON notifications
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_updated_at();
