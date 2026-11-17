package app

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func maintenanceTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store
}

func TestHistoryRetentionPreservesAccountsLinksAndRecentConversations(t *testing.T) {
	ctx := context.Background()
	s := maintenanceTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	old := now.Add(-11 * 24 * time.Hour)
	cutoff := now.Add(-historyRetention)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"old", "recent", "live-lease", "empty"} {
		exec(`INSERT INTO wa_sqlite_message_sessions VALUES (?, 'account', 'profile', 'identity', '', 'MESSAGE_SESSION_STATUS_CLOSED', ?, '{}')`, name, old.UnixNano())
	}
	for _, row := range []struct {
		id, session, ref string
		at               time.Time
	}{{"old-message", "old", "old-ref", old}, {"new-message", "recent", "keep-ref", now}, {"boundary-message", "recent", "boundary-ref", cutoff}} {
		msg := &waappv1.InboundMessage{MessageId: row.id, MessageSessionId: row.session, ContactRef: row.ref, Kind: waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE, ReceivedAt: timestamppb.New(row.at)}
		if err := s.SaveInboundMessages(ctx, []*waappv1.InboundMessage{msg}); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO wa_sqlite_candidates VALUES ('candidate-old','old-message','decrypted-old',?,'{}')`, old.UnixNano())
	exec(`INSERT INTO wa_sqlite_decrypted_messages VALUES ('decrypted-old','old-message',?,'{}')`, old.UnixNano())
	exec(`INSERT INTO wa_sqlite_decrypted_messages VALUES ('decrypted-new','new-message',?,'{}')`, now.UnixNano())
	exec(`INSERT INTO wa_sqlite_otp_messages VALUES ('otp-old','account',?,'{}')`, old.UnixNano())
	exec(`INSERT INTO wa_sqlite_otp_messages VALUES ('otp-new','account',?,'{}')`, now.UnixNano())
	for _, row := range []struct {
		id, account, ref string
		at               time.Time
	}{{"delete-contact", "account", "old-ref", old}, {"keep-contact", "account", "keep-ref", old}, {"other-account", "another", "keep-ref", old}, {"new-contact", "account", "new-ref", now}, {"boundary-contact", "account", "boundary-ref", cutoff}} {
		contact := &waappv1.WAContact{ContactId: row.id, WaAccountId: row.account, Jid: row.ref, Audit: &waappv1.AuditStamp{UpdatedAt: timestamppb.New(row.at)}}
		payload, err := sqliteMarshal(contact)
		if err != nil {
			t.Fatal(err)
		}
		exec(`INSERT INTO wa_sqlite_contacts VALUES (?,?,?,?)`, row.id, row.account, row.at.UnixNano(), payload)
	}
	exec(`INSERT INTO wa_sqlite_runtime_state VALUES ('transient','sms-receiver-code-link:test-link',x'01',?)`, now.AddDate(100, 0, 0).UnixNano())
	exec(`INSERT INTO wa_sqlite_runtime_state VALUES ('transient','sms-receiver-code-link-owner:test-owner',x'02',?)`, now.AddDate(100, 0, 0).UnixNano())
	exec(`INSERT INTO wa_sqlite_runtime_state VALUES ('message-session','live-lease',x'03',?)`, now.Add(time.Hour).UnixNano())
	exec(`INSERT INTO wa_sqlite_runtime_state VALUES ('transient','sms-receiver-code-link-session:expired',x'04',?)`, old.UnixNano())
	exec(`INSERT INTO wa_sqlite_accounts VALUES ('account','+12025550100',?,'{"status":"WA_ACCOUNT_STATUS_ACTIVE"}')`, old.UnixNano())
	exec(`INSERT INTO wa_sqlite_native_states VALUES ('profile','{"fixture":"preserve"}',?)`, old.UnixNano())
	exec(`INSERT INTO wa_sqlite_login_states VALUES ('login','registration','account','profile','identity','LOGIN_STATE_STATUS_ACTIVE',?,'{}')`, old.UnixNano())
	exec(`INSERT INTO wa_sqlite_client_profiles VALUES ('profile','account','','',?,'{}')`, old.UnixNano())
	exec(`INSERT INTO wa_sqlite_registrations VALUES ('registration','verification','account','profile',?,'{}')`, old.UnixNano())
	exec(`INSERT INTO wa_sqlite_verification_requests VALUES ('verification','account','profile',?,'{}')`, old.UnixNano())
	for i := 0; i < 4; i++ {
		removed, err := s.pruneHistoryBatch(ctx, now)
		if err != nil {
			t.Fatal(err)
		}
		if removed == 0 {
			break
		}
	}
	for table, want := range map[string]int{"accounts": 1, "native_states": 1, "login_states": 1, "client_profiles": 1, "registrations": 1, "verification_requests": 1, "inbound_messages": 2, "contacts": 3, "message_sessions": 2, "candidates": 0, "decrypted_messages": 1, "otp_messages": 1, "runtime_state": 3} {
		var count int
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM wa_sqlite_`+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != want {
			t.Errorf("%s rows=%d want=%d", table, count, want)
		}
	}
	var native string
	if err := s.db.QueryRowContext(ctx, `SELECT state_json FROM wa_sqlite_native_states`).Scan(&native); err != nil || native != `{"fixture":"preserve"}` {
		t.Fatal("native credentials changed")
	}
	for _, key := range []string{"sms-receiver-code-link:test-link", "sms-receiver-code-link-owner:test-owner"} {
		var count int
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM wa_sqlite_runtime_state WHERE key=?`, key).Scan(&count); err != nil || count != 1 {
			t.Fatal("valid link removed")
		}
	}
	removed, err := s.pruneHistoryBatch(ctx, now)
	if err != nil || removed != 0 {
		t.Fatalf("cleanup not idempotent: removed=%d err=%v", removed, err)
	}
}

func TestStaleSessionCleanupIsBoundedAndRespectsLiveLeases(t *testing.T) {
	s := maintenanceTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for i := 0; i < 260; i++ {
		id := fmt.Sprintf("stale-%03d", i)
		session := &waappv1.MessageSession{MessageSessionId: id, Status: waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_OPEN, OpenedAt: timestamppb.New(now.Add(-time.Hour))}
		if err := s.SaveMessageSession(ctx, session); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO wa_sqlite_runtime_state VALUES ('message-session','stale-000',x'01',?)`, now.Add(time.Hour).UnixNano()); err != nil {
		t.Fatal(err)
	}
	closed, err := s.CloseStaleOpenMessageSessions(ctx, now.Add(-10*time.Minute))
	if err != nil || closed != 250 {
		t.Fatalf("first batch=%d err=%v", closed, err)
	}
	closed, err = s.CloseStaleOpenMessageSessions(ctx, now.Add(-10*time.Minute))
	if err != nil || closed != 9 {
		t.Fatalf("second batch=%d err=%v", closed, err)
	}
	active, err := s.GetMessageSession(ctx, "stale-000")
	if err != nil || active.GetStatus() != waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_OPEN {
		t.Fatal("active lease was closed")
	}
}

func TestClaimRequestReclaimsOnlyItsExpiredKey(t *testing.T) {
	ctx := context.Background()
	r, err := NewSQLiteRuntimeFile(ctx, filepath.Join(t.TempDir(), "runtime.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for i, want := range []bool{true, false} {
		claimed, err := r.ClaimRequest(ctx, "test-request", time.Minute)
		if err != nil || claimed != want {
			t.Fatalf("claim %d=%v err=%v", i, claimed, err)
		}
	}
	if _, err := r.db.ExecContext(ctx, `UPDATE wa_sqlite_runtime_state SET expires_at=1 WHERE key='test-request'`); err != nil {
		t.Fatal(err)
	}
	claimed, err := r.ClaimRequest(ctx, "test-request", time.Minute)
	if err != nil || !claimed {
		t.Fatal("expired request cannot be reclaimed")
	}
}

func TestNativePayloadRetentionKeepsCredentialsAndPendingCache(t *testing.T) {
	now := time.Now().UTC()
	state := nativeState{
		AuthKey:    "synthetic-auth-key",
		ChatStatic: nativeCurveKeyPair{Private: "synthetic-private", Public: "synthetic-public"},
		MessagePayloads: map[string]nativeMessagePayload{
			"old":      {StoredAtUnix: now.Add(-historyRetention - time.Hour).Unix(), Payload: "old"},
			"recent":   {StoredAtUnix: now.Unix(), Payload: "recent"},
			"boundary": {StoredAtUnix: now.Add(-historyRetention).Unix(), Payload: "boundary"},
			"legacy":   {Payload: "pending"},
		},
		MessagePlainRef: map[string]string{"old": "old-ref", "recent": "recent-ref"},
	}
	pruneNativeMessagePayloads(&state, now)
	if len(state.MessagePayloads) != 3 || state.MessagePayloads["legacy"].StoredAtUnix != now.Unix() {
		t.Fatal("recent or undated pending cache was lost")
	}
	if _, exists := state.MessagePlainRef["old"]; exists {
		t.Fatal("expired cache reference was retained")
	}
	if state.AuthKey != "synthetic-auth-key" || state.ChatStatic.Private != "synthetic-private" {
		t.Fatal("credentials changed")
	}
	pruneNativeMessagePayloads(&state, now.Add(historyRetention+time.Hour))
	if len(state.MessagePayloads) != 0 {
		t.Fatal("legacy cache did not expire after its grace period")
	}
}
