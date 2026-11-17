package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// The opt-in probe reads one existing account without writing its database.
// Stop its regular connection first. All session updates remain in memory.
type chatdProbeStateStore struct {
	state nativeState
}

func (s *chatdProbeStateStore) GetNativeState(context.Context, string) (nativeState, error) {
	return s.state, nil
}

func (s *chatdProbeStateStore) SaveNativeState(_ context.Context, _ string, state nativeState) error {
	s.state = state
	return nil
}

func TestLiveChatdCompatibility(t *testing.T) {
	path := os.Getenv("WA_CHATD_LIVE_PROBE_FILE")
	if path == "" {
		t.Skip("live connection probe is opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal("open read-only probe database failed")
	}
	defer db.Close()
	offset := 0
	if value := os.Getenv("WA_CHATD_LIVE_PROBE_OFFSET"); value != "" {
		offset, err = strconv.Atoi(value)
		if err != nil || offset < 0 || offset > 4 {
			t.Fatal("probe offset must be between zero and four")
		}
	}
	var loginJSON, stateJSON string
	err = db.QueryRowContext(ctx, `SELECT l.payload,n.state_json FROM wa_sqlite_login_states l
JOIN wa_sqlite_native_states n ON n.client_profile_id=l.client_profile_id
JOIN wa_sqlite_accounts a ON a.id=l.wa_account_id
WHERE l.status='LOGIN_STATE_STATUS_ACTIVE' ORDER BY l.wa_account_id LIMIT 1 OFFSET ?`, offset).Scan(&loginJSON, &stateJSON)
	if err != nil {
		t.Fatal("select probe account failed")
	}
	login := &waappv1.LoginState{}
	if err := protojson.Unmarshal([]byte(loginJSON), login); err != nil {
		t.Fatal("decode probe login failed")
	}
	store := &chatdProbeStateStore{}
	if err := json.Unmarshal([]byte(stateJSON), &store.state); err != nil {
		t.Fatal("decode probe state failed")
	}
	engine, err := NewNativeEngine(store, nil, nil)
	if err != nil {
		t.Fatal("create probe engine failed")
	}
	if proxy := os.Getenv("WA_COMMON_PROXY"); proxy != "" {
		engine, err = engine.WithProxyURL(proxy)
		if err != nil {
			t.Fatal("configure probe route failed")
		}
	}
	defer engine.CloseIdleConnections()
	result := engine.CheckLoginState(ctx, EngineLoginCheckInput{
		WAAccountID: login.GetWaAccountId(), ClientProfileID: login.GetClientProfileId(),
		RegisteredIdentityID: login.GetRegisteredIdentityId(), AppVersion: defaultWAAppVersion,
		RemoteTimeout: 15 * time.Second,
	})
	if result.Err != nil || result.Status != waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_ACTIVE {
		t.Fatalf("probe failed: status=%s version_rejected=%t credential_rejection=%q", result.Status, isChatdClientOutdated(result.Err), chatdLoginRejectionCode(result.Err))
	}
	t.Logf("explicit login success; connect_version=%s; database_writes=0", defaultChatdAppVersion)
}
