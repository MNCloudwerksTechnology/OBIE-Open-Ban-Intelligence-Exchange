-- Inquiries sent through the website's form (WP #1674, ADR 0012).
-- The client IP is stored only as a salted HMAC, never in clear. The mail_*
-- columns make the table its own outbox: a scheduled dispatcher sends the
-- notification and the confirmation and retries failures with backoff.
CREATE TABLE inquiry (
    id                      UUID          PRIMARY KEY,
    created_at              TIMESTAMPTZ   NOT NULL,
    type                    VARCHAR(20)   NOT NULL,
    name                    VARCHAR(200)  NOT NULL,
    email                   VARCHAR(254)  NOT NULL,
    organisation            VARCHAR(200),
    event_date              DATE,
    event_location          VARCHAR(200),
    audience_size           INTEGER,
    message                 VARCHAR(5000) NOT NULL,
    status                  VARCHAR(20)   NOT NULL,
    client_ip_hash          VARCHAR(64)   NOT NULL,
    notification_sent_at    TIMESTAMPTZ,
    confirmation_sent_at    TIMESTAMPTZ,
    mail_attempts           INTEGER       NOT NULL DEFAULT 0,
    next_mail_attempt_at    TIMESTAMPTZ,
    CONSTRAINT inquiry_type_check
        CHECK (type IN ('TALK', 'WORKSHOP', 'INTERVIEW', 'COLLABORATION', 'OTHER')),
    CONSTRAINT inquiry_status_check CHECK (status IN ('NEW', 'ANSWERED'))
);

-- The retention job deletes by age; the dispatcher polls for due mails.
CREATE INDEX inquiry_created_at_idx ON inquiry (created_at);
CREATE INDEX inquiry_next_mail_attempt_at_idx ON inquiry (next_mail_attempt_at)
    WHERE next_mail_attempt_at IS NOT NULL;
