package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func TestConnectionPayloadUsesSupportedVersion(t *testing.T) {
	for _, test := range []struct {
		version string
		want    [5]uint64
	}{
		{"", [5]uint64{2, 26, 40, 76, 0}},
		{"2.26.24.77", [5]uint64{2, 26, 40, 76, 0}},
		{"2.26.40.76", [5]uint64{2, 26, 40, 76, 0}},
		{"2.26.41.1", [5]uint64{2, 26, 41, 1, 0}},
	} {
		for _, builder := range []func(loginIdentity, nativeState, string) []byte{defaultLoginPayload, passiveLoginCheckPayload} {
			payload := builder(loginIdentity{}, nativeState{}, test.version)
			fields, err := parsePBFields(payload)
			if err != nil {
				t.Fatal(err)
			}
			ua, err := parsePBFields(optionalPBBytes(fields, 5))
			if err != nil {
				t.Fatal(err)
			}
			platform, ok := optionalPBVarint(ua, 1)
			if !ok || platform != 0 {
				t.Fatal("connection did not announce Android")
			}
			version, err := parsePBFields(optionalPBBytes(ua, 2))
			if err != nil {
				t.Fatal(err)
			}
			for i, want := range test.want {
				got, ok := optionalPBVarint(version, i+1)
				if !ok || got != want {
					t.Fatalf("version %q field %d: got %d, want %d", test.version, i+1, got, want)
				}
			}
		}
	}
	if !strings.HasPrefix(nativeUserAgentForState(nativeState{}, "2.26.24.77"), "WhatsApp/2.26.24.77 ") {
		t.Fatal("connection update changed registration's APK version")
	}
}

func TestReadingProfileDoesNotDowngradeVersion(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	profile := &waappv1.ProtocolProfile{ProtocolProfileId: "waproto_native", AppVersion: "2.26.41.1"}
	if err := store.SaveProtocolProfile(ctx, profile); err != nil {
		t.Fatal(err)
	}
	server := &Server{store: store}
	if got := server.protocolIDAppVersion(ctx, profile.GetProtocolProfileId()); got != profile.GetAppVersion() {
		t.Fatalf("protocol version downgraded to %s", got)
	}
	if got := server.clientProfileAppVersion(ctx, &waappv1.ClientProfile{ProtocolProfileId: profile.GetProtocolProfileId()}); got != profile.GetAppVersion() {
		t.Fatalf("client version downgraded to %s", got)
	}
	stored, err := store.GetProtocolProfile(ctx, profile.GetProtocolProfileId())
	if err != nil || stored.GetAppVersion() != profile.GetAppVersion() {
		t.Fatal("version lookup changed the database")
	}
}

func TestVersionRejectionStopsWithoutInvalidatingLogin(t *testing.T) {
	err := errors.New("chatd endpoint example.invalid failed: server sent failure type=auth reason=405 location=test")
	mapped := chatdReceiveError(err)
	if !longConnectionTerminalError(mapped) || ToProtoError(mapped).GetRetryable() {
		t.Fatal("version rejection still retryable")
	}
	if chatdLoginRejectionCode(mapped) != "" || loginCheckStatusForError(mapped) != waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNREACHABLE {
		t.Fatal("version rejection treated as invalid credentials")
	}
	if strings.Contains(mapped.Error(), "example.invalid") || strings.Contains(mapped.Error(), "location=") {
		t.Fatal("version error exposed upstream metadata")
	}
}
