-- Reverses 20260910000015_idempotency_records.
--
-- Dropping the table loses in-flight idempotency guarantees: a caller that
-- retries across the rollback gets a second execution rather than the stored
-- result. That is the cost of removing the feature and is why the up migration
-- is additive — rolling the APPLICATION back does not require this.
DROP FUNCTION IF EXISTS sweep_idempotency_records(timestamptz, int);
DROP TABLE IF EXISTS idempotency_records;
