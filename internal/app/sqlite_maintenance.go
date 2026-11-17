package app

import (
	"context"
	"log"
	"time"
)

const historyRetention = 10 * 24 * time.Hour

// RunMaintenance bounds both retention and writer lock duration. Freed SQLite pages
// are reused; a full VACUUM would block live account and message operations.
func (s *SQLiteStore) RunMaintenance(ctx context.Context) error {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		cycleCtx, cancel := context.WithTimeout(ctx, time.Minute)
		now := time.Now().UTC()
		var total int64
		for cycleCtx.Err() == nil {
			removed, err := s.pruneHistoryBatch(cycleCtx, now)
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("WA history cleanup failed: %v", sanitizeLogError(err))
				}
				break
			}
			total += removed
			if removed == 0 || !sleepContext(cycleCtx, 25*time.Millisecond) {
				break
			}
		}
		cancel()
		if total > 0 {
			log.Printf("WA history cleanup removed=%d retention_days=10", total)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

const oldHistoryMessagesSQL = `SELECT id FROM wa_sqlite_inbound_messages WHERE received_at<? ORDER BY received_at, id LIMIT 250`

func (s *SQLiteStore) pruneHistoryBatch(ctx context.Context, now time.Time) (int64, error) {
	cutoff := now.Add(-historyRetention)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	statements := []struct {
		query string
		args  []any
	}{
		{`DELETE FROM wa_sqlite_candidates WHERE message_id IN (` + oldHistoryMessagesSQL + `)`, []any{cutoff.UnixNano()}},
		{`DELETE FROM wa_sqlite_decrypted_messages WHERE message_id IN (` + oldHistoryMessagesSQL + `)`, []any{cutoff.UnixNano()}},
		{`DELETE FROM wa_sqlite_otp_messages WHERE json_extract(payload, '$.message_id') IN (` + oldHistoryMessagesSQL + `)`, []any{cutoff.UnixNano()}},
		{`DELETE FROM wa_sqlite_inbound_messages WHERE id IN (` + oldHistoryMessagesSQL + `)`, []any{cutoff.UnixNano()}},
		{`DELETE FROM wa_sqlite_candidates WHERE id IN (
 SELECT c.id FROM wa_sqlite_candidates c WHERE c.extracted_at<?
 AND NOT EXISTS (SELECT 1 FROM wa_sqlite_inbound_messages m WHERE m.id=c.message_id)
 ORDER BY c.extracted_at, c.id LIMIT 250)`, []any{cutoff.UnixNano()}},
		{`DELETE FROM wa_sqlite_decrypted_messages WHERE id IN (
 SELECT d.id FROM wa_sqlite_decrypted_messages d WHERE d.decrypted_at<?
 AND NOT EXISTS (SELECT 1 FROM wa_sqlite_inbound_messages m WHERE m.id=d.message_id)
 ORDER BY d.decrypted_at, d.id LIMIT 250)`, []any{cutoff.UnixNano()}},
		{`DELETE FROM wa_sqlite_otp_messages WHERE id IN (
 SELECT id FROM wa_sqlite_otp_messages WHERE received_at<? ORDER BY received_at, id LIMIT 250)`, []any{cutoff.UnixNano()}},
		{`DELETE FROM wa_sqlite_contacts WHERE id IN (
 SELECT c.id FROM wa_sqlite_contacts c
 WHERE COALESCE(unixepoch(json_extract(c.payload, '$.last_message_at')), unixepoch(json_extract(c.payload, '$.audit.updated_at')), c.updated_at / 1000000000)<?
 AND NOT EXISTS (
  SELECT 1 FROM wa_sqlite_inbound_messages m CROSS JOIN wa_sqlite_message_sessions s ON s.id=m.message_session_id
  WHERE json_extract(m.payload, '$.kind')='INBOUND_MESSAGE_KIND_MESSAGE'
   AND COALESCE(NULLIF(json_extract(m.payload, '$.contact_ref'), ''), json_extract(m.payload, '$.sender_ref')) IN (
    c.id, json_extract(c.payload, '$.jid'), ltrim(json_extract(c.payload, '$.number'), '+'),
    ltrim(json_extract(c.payload, '$.number'), '+') || '@s.whatsapp.net', '+' || ltrim(json_extract(c.payload, '$.number'), '+'))
   AND m.received_at>=? AND s.wa_account_id=c.wa_account_id
   AND COALESCE(json_extract(m.payload, '$.delete_status'), '')<>'MESSAGE_DELETE_STATUS_DELETED_FOR_ME'
 ) LIMIT 250)`, []any{cutoff.Unix(), cutoff.UnixNano()}},
		{`DELETE FROM wa_sqlite_message_sessions WHERE id IN (
 SELECT s.id FROM wa_sqlite_message_sessions s
 WHERE s.status IN ('MESSAGE_SESSION_STATUS_CLOSED', 'MESSAGE_SESSION_STATUS_FAILED') AND s.updated_at<?
 AND NOT EXISTS (SELECT 1 FROM wa_sqlite_inbound_messages m WHERE m.message_session_id=s.id)
 AND NOT EXISTS (SELECT 1 FROM wa_sqlite_runtime_state r WHERE r.kind='message-session' AND r.key=s.id AND r.expires_at>?)
 ORDER BY s.updated_at, s.id LIMIT 250)`, []any{now.Add(-24 * time.Hour).UnixNano(), now.UnixNano()}},
		{`DELETE FROM wa_sqlite_runtime_state WHERE rowid IN (
 SELECT rowid FROM wa_sqlite_runtime_state WHERE expires_at<=? ORDER BY expires_at LIMIT 250)`, []any{now.UnixNano()}},
	}
	var total int64
	for _, statement := range statements {
		result, err := tx.ExecContext(ctx, statement.query, statement.args...)
		if err != nil {
			return 0, err
		}
		removed, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		total += removed
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return total, nil
}
