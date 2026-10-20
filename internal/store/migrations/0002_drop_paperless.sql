-- Paperless-ngx gave way to Pocket Documents (an app link, app.documents.*).
-- The typed-in Paperless address and token nobody reads any more go.
DELETE FROM user_settings WHERE key LIKE 'svc.paperless.%';
