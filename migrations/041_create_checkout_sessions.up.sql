CREATE TABLE IF NOT EXISTS checkout_sessions (
    checkout_reference VARCHAR(64) PRIMARY KEY,
    shopify_draft_order_id VARCHAR(128) NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    user_id UUID,
    session_id VARCHAR(128),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_checkout_sessions_expires_at
    ON checkout_sessions (expires_at);
