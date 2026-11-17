package app

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/proto"
)

type rejectedConnectionEngine struct {
	ProtocolEngine
	calls      int
	checkCalls int
	code       string
}

func (e *rejectedConnectionEngine) CheckLoginState(context.Context, EngineLoginCheckInput) EngineLoginCheckResult {
	e.checkCalls++
	err := chatdReceiveError(errors.New("server sent failure reason=" + e.code))
	return EngineLoginCheckResult{Status: loginCheckStatusForError(err), Err: err}
}

func (e *rejectedConnectionEngine) ReceiveMessageBatch(context.Context, EngineMessageInput) EngineMessageBatchResult {
	e.calls++
	return EngineMessageBatchResult{Err: chatdReceiveError(errors.New("server sent failure reason=" + e.code))}
}

func TestRejectedConnectionStopsAndRestoresFailure(t *testing.T) {
	for _, code := range []string{"401", "403", "405"} {
		t.Run(code, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			dir := t.TempDir()
			store, err := NewSQLiteStore(ctx, dir)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			runtime, err := NewSQLiteRuntime(ctx, dir)
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()
			account := &waappv1.WAAccount{WaAccountId: "test-account", Phone: &waappv1.PhoneTarget{E164Number: "+12025550100"}, Status: waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_ACTIVE}
			profile := &waappv1.ClientProfile{ClientProfileId: "test-profile", WaAccountId: account.GetWaAccountId()}
			login := &waappv1.LoginState{LoginStateId: "test-login", RegistrationId: "test-registration", WaAccountId: account.GetWaAccountId(), ClientProfileId: profile.GetClientProfileId(), RegisteredIdentityId: "test-identity", Status: waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE}
			if err := store.SaveWAAccount(ctx, account); err != nil {
				t.Fatal(err)
			}
			if err := store.SaveClientProfile(ctx, profile); err != nil {
				t.Fatal(err)
			}
			if err := store.SaveLoginState(ctx, login, ""); err != nil {
				t.Fatal(err)
			}
			engine := &rejectedConnectionEngine{code: code}
			server := NewServer(store, runtime, engine, nil, nil)
			manager := server.longConnections
			key := longConnectionKey(login)
			manager.entries[key] = &longConnectionEntry{cancel: func() {}, snapshot: &waappv1.LongConnectionState{WaAccountId: account.GetWaAccountId()}}
			manager.runEntry(ctx, login, key)
			if ctx.Err() != nil || engine.calls != 1 {
				t.Fatalf("rejection retried: calls=%d err=%v", engine.calls, ctx.Err())
			}
			if manager.entries[key].snapshot.GetStatus() != waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_FAILED || manager.entries[key].cancel != nil {
				t.Fatal("failed connection stayed active")
			}
			stored, err := store.GetLoginState(ctx, login.GetLoginStateId())
			if code == "405" {
				if err != nil || !proto.Equal(stored, login) {
					t.Fatal("version rejection changed the saved login")
				}
				manager.reconcileStoppedAccounts(ctx)
				if engine.calls != 1 || engine.checkCalls != 0 {
					t.Fatal("reconcile retried an unsupported client version")
				}
				check := &waappv1.LoginStateCheck{Status: waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNREACHABLE, Error: manager.entries[key].snapshot.GetLastError()}
				server.applyLoginStateCheck(stored, check, time.Now())
				if stored.GetStatus() != waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE {
					t.Fatal("version rejection invalidated credentials")
				}
				return
			}
			if err != nil || stored.GetStatus() != waappv1.LoginStateStatus_LOGIN_STATE_STATUS_INVALID {
				t.Fatalf("rejection was not persisted: %v", err)
			}
			if stored.GetLastError().GetRetryable() {
				t.Fatal("rejected credentials still retryable")
			}
			preserved, err := store.GetWAAccount(ctx, account.GetWaAccountId())
			if err != nil || preserved.GetStatus() != waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_ACTIVE {
				t.Fatal("account was removed or marked transferred")
			}
			restarted := NewLongConnectionManager(server)
			restarted.rootCtx = ctx
			if err := restarted.restore(ctx); err != nil {
				t.Fatal(err)
			}
			states := restarted.Snapshots(&waappv1.GetLongConnectionStatusRequest{})
			if len(states) != 1 || states[0].GetStatus() != waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_FAILED {
				t.Fatal("restart lost rejection state")
			}
			if engine.calls != 1 {
				t.Fatal("restart retried rejected login")
			}
		})
	}
}

func TestLoginRejectionDoesNotMatchNetworkOrTemporaryErrors(t *testing.T) {
	for _, message := range []string{"proxy returned 403", "proxy returned 405", "server sent failure reason=503", "server sent failure reason=4010", "server sent failure reason=4050", "chatd dial timeout", "server sent stream:error <conflict type=replaced>"} {
		err := chatdReceiveError(errors.New(message))
		if longConnectionTerminalError(err) || !ToProtoError(err).GetRetryable() {
			t.Errorf("temporary error treated as rejected login: %s", message)
		}
	}
	err := errors.New("chatd endpoint example.invalid failed: server sent failure type=auth reason=401 location=test")
	if loginCheckStatusForError(err) != waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_INVALID {
		t.Fatal("login rejection misclassified as network error")
	}
}

func TestLoginCheckRequiresExplicitSuccess(t *testing.T) {
	for _, test := range []struct {
		name      string
		nodes     []chatdNode
		readError error
		want      waappv1.LoginStateCheckStatus
	}{
		{"timeout", nil, errors.New("i/o timeout"), waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNREACHABLE},
		{"unrelated-node", []chatdNode{{Tag: "iq"}}, io.EOF, waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNREACHABLE},
		{"refused-after-control", []chatdNode{{Tag: "ib"}, {Tag: "failure", Attrs: map[string]string{"reason": "401"}}}, io.EOF, waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_INVALID},
		{"outdated-client", []chatdNode{{Tag: "failure", Attrs: map[string]string{"reason": "405"}}}, io.EOF, waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNREACHABLE},
		{"success-after-control", []chatdNode{{Tag: "ib"}, {Tag: "success"}}, io.EOF, waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_ACTIVE},
	} {
		t.Run(test.name, func(t *testing.T) {
			next := 0
			_, err := readLoginCheckResult(func() (chatdNode, error) {
				if next >= len(test.nodes) {
					return chatdNode{}, test.readError
				}
				node := test.nodes[next]
				next++
				return node, nil
			}, chatdSessionUpdate{})
			if got := loginCheckStatusForError(err); got != test.want {
				t.Fatalf("login result=%s want=%s", got, test.want)
			}
		})
	}
}

func TestUnreachableCheckPreservesKnownLoginRejection(t *testing.T) {
	state := &waappv1.LoginState{Status: waappv1.LoginStateStatus_LOGIN_STATE_STATUS_INVALID, LastError: ToProtoError(chatdReceiveError(errors.New("server sent failure reason=403")))}
	check := &waappv1.LoginStateCheck{Status: waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNREACHABLE, Error: ToProtoError(errors.New("timeout"))}
	server := &Server{}
	server.applyLoginStateCheck(state, check, time.Now())
	if state.GetStatus() != waappv1.LoginStateStatus_LOGIN_STATE_STATUS_INVALID || chatdLoginRejectionCode(errorFromProto(state.GetLastError())) != "403" {
		t.Fatal("inconclusive probe erased known login rejection")
	}
}
