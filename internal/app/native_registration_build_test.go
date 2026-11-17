package app

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	protocolv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/proto"
)

func registrationFixture(t *testing.T) (*NativeEngine, *chatdProbeStateStore, *waappv1.PhoneTarget) {
	t.Helper()
	phone := &waappv1.PhoneTarget{E164Number: "+12025550123", CountryCallingCode: "1", NationalNumber: "2025550123", CountryIso2: "US"}
	state, err := newNativeState(phone)
	if err != nil {
		t.Fatal("create synthetic registration state failed")
	}
	store := &chatdProbeStateStore{state: state}
	engine, err := NewNativeEngine(store, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.CloseIdleConnections)
	return engine, store, phone
}

func TestRegistrationBuildRejectsInconsistentMaterial(t *testing.T) {
	build, err := androidRegistrationBuild()
	if err != nil {
		t.Fatal(err)
	}
	// Independently calculated from the pinned upstream bundle using Python's
	// protobuf wire decoder and HMAC implementation; only hashes enter diagnostics.
	bundleHash := sha256.Sum256(androidRegistrationBuildData)
	tokenHash := sha256.Sum256([]byte(deriveDefaultRegistrationToken("2025550123")))
	if hex.EncodeToString(bundleHash[:]) != "1c66baccb94e3da10c9258841a9ff61f1b24674d5023ffff0c20c5b126804c4d" || hex.EncodeToString(tokenHash[:]) != "94521aa58499bce6ccbd0b9bd6888201b963de26ba6c3be0dcd8ca913473cbfa" {
		t.Fatal("registration bundle or token differs from the upstream reference")
	}
	for name, mutate := range map[string]func(*protocolv1.AndroidBuildMaterial){
		"version":            func(b *protocolv1.AndroidBuildMaterial) { b.AppVersion = "2.26.24.77" },
		"version code":       func(b *protocolv1.AndroidBuildMaterial) { b.VersionCode++ },
		"token key":          func(b *protocolv1.AndroidBuildMaterial) { b.TokenHmacKey = nil },
		"dex":                func(b *protocolv1.AndroidBuildMaterial) { b.ClassesDexMd5 = nil },
		"certificate":        func(b *protocolv1.AndroidBuildMaterial) { b.SigningCertificate = nil },
		"integrity":          func(b *protocolv1.AndroidBuildMaterial) { b.ApplicationIntegrity = nil },
		"certificate digest": func(b *protocolv1.AndroidBuildMaterial) { b.ApplicationIntegrity.CertificateSha1[0] ^= 1 },
		"server":             func(b *protocolv1.AndroidBuildMaterial) { b.ServerPublicKey[0] ^= 1 },
	} {
		t.Run(name, func(t *testing.T) {
			copy := proto.Clone(build).(*protocolv1.AndroidBuildMaterial)
			mutate(copy)
			if validateAndroidRegistrationBuild(copy) == nil {
				t.Fatal("inconsistent registration material accepted")
			}
		})
	}
	for _, version := range []string{"", "2.26.24.77", "2.26.40.76"} {
		got, err := registrationAppVersion(version)
		if err != nil || got != build.GetAppVersion() {
			t.Fatal("legacy profile did not select the matching build")
		}
	}
	if _, err := registrationAppVersion("2.26.41.1"); err == nil {
		t.Fatal("unbundled registration version accepted")
	}
	if deriveDefaultRegistrationToken("") != "" || deriveDefaultRegistrationToken("not-a-number") != "" {
		t.Fatal("invalid number generated a token")
	}
}

func TestRegistrationRequestsUseMatchingMaterialAndContinuation(t *testing.T) {
	engine, store, phone := registrationFixture(t)
	state := store.state
	state.PreChatdAB.ExpConfig = `[{"config_code":3753,"config_value":1},{"config_code":7999,"config_value":1}]`
	state.LastCodeResult = sanitizeResponse(map[string]any{"auth_response": "synthetic-proof", "context": "synthetic-context", "login": "synthetic-login"})
	updateRegistrationVerification(&state, map[string]any{"auth_response": "synthetic-proof", "context": "synthetic-context", "login": "synthetic-login"})
	encoded, err := json.Marshal(state)
	if err != nil || json.Unmarshal(encoded, &state) != nil {
		t.Fatal("registration continuation did not round-trip")
	}
	build, _ := androidRegistrationBuild()
	base := []string{"cc", "in", "lg", "lc", "fdid", "expid", "access_session_id", "id", "backup_token"}
	for _, kind := range []string{"exist", "code", "register"} {
		t.Run(kind, func(t *testing.T) {
			var params orderedParams
			var err error
			switch kind {
			case "exist":
				params, err = engine.existOrderedParams(t.Context(), phone, state, nil, true)
			case "code":
				params, err = engine.codeRequestOrderedParams(t.Context(), phone, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS, state, "")
			case "register":
				params, err = engine.registerOrderedParams(t.Context(), phone, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS, "000000", state, "", nil, true)
			}
			if err != nil {
				t.Fatal(err)
			}
			keys := make([]string, len(params))
			for i, param := range params {
				keys[i] = param.key
			}
			if len(keys) < len(base) || !slices.Equal(keys[:len(base)], base) {
				t.Fatal("registration prefix order changed")
			}
			decoded, err := url.ParseQuery(params.render())
			if err != nil || decoded.Get("type") != "0" || decoded.Get("authkey") != state.AuthKey {
				t.Fatal("request encoding corrupted identity fields")
			}
			if kind == "register" {
				if decoded.Has("token") || decoded.Get("auth_response") != "synthetic-proof" || decoded.Get("context") != "synthetic-context" {
					t.Fatal("verification did not preserve continuation or retained build token")
				}
			} else if decoded.Get("token") == "" || decoded.Get("token") != deriveDefaultRegistrationToken(phone.GetNationalNumber()) {
				t.Fatal("registration token was missing or mismatched")
			}
			if kind == "exist" && decoded.Has("mcc") || decoded.Has("rc2") || decoded.Has("airplane_mode_on") {
				t.Fatal("obsolete request fields present")
			}
			primary := decodeRegistrationGPIA(t, state, decoded.Get("gpia"))
			compact := decodeRegistrationGPIA(t, state, decoded.Get("_gi"))
			material := build.GetApplicationIntegrity()
			if primary["sha256"] != base64.StdEncoding.EncodeToString(material.GetApkSha256()) || primary["sizeInBytes"] != strconv.FormatUint(material.GetApkSize(), 10) || compact["_dh"] != base64.StdEncoding.EncodeToString(material.GetPrimaryDexSha256()) {
				t.Fatal("GPIA mixed APK builds")
			}
			if primary["code"] != float64(nativeIntegrityUnavailableCode) || primary["token"] != nil {
				t.Fatal("unavailable device SDK fabricated a success or a device status")
			}
		})
	}
	state.PreChatdAB.ExpConfig = `[{"config_code":3753,"config_value":0}]`
	params, err := engine.codeRequestOrderedParams(t.Context(), phone, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS, state, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range params {
		if slices.Contains([]string{"gpia", "_gi", "_gg"}, field.key) {
			t.Fatal("disabled integrity gate emitted material")
		}
	}
	state.VerificationContext = nil
	if registrationVerificationValue(state, "auth_response") != "" {
		t.Fatal("redacted legacy value used as registration proof")
	}
}

func decodeRegistrationGPIA(t *testing.T, state nativeState, encoded string) map[string]any {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) < 32 {
		t.Fatal("missing encrypted integrity material")
	}
	key := sha256.Sum256([]byte(nativeGPIAKeySource(state)))
	plain, err := aesCBCPKCS7Decrypt(raw[16:], key[:], raw[:16])
	if err != nil {
		t.Fatal("integrity material could not be decrypted")
	}
	var fields map[string]any
	if json.Unmarshal(plain, &fields) != nil {
		t.Fatal("integrity payload is invalid JSON")
	}
	return fields
}

func TestRegistrationRuntimeAnnouncesBundledBuild(t *testing.T) {
	engine, store, phone := registrationFixture(t)
	observed := make(chan string, 4)
	engine.http.dialTLSContext = func(context.Context, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			req, err := http.ReadRequest(bufio.NewReader(server))
			if err != nil {
				observed <- "read-failed"
				return
			}
			body, err := io.ReadAll(req.Body)
			_ = req.Body.Close()
			if err != nil || req.Header.Get("Authorization") != "" || strings.Contains(string(body), "&H=") {
				observed <- "unexpected-attestation"
			} else {
				observed <- req.Header.Get("User-Agent")
			}
			response := `{"status":"sent","auth_response":"synthetic-server-proof","context":"synthetic-context"}`
			if strings.Contains(req.URL.RawQuery, "register") {
				response = `{"status":"ok","login":"12025550123"}`
			}
			_, _ = fmt.Fprintf(server, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(response), response)
		}()
		return client, nil
	}
	input := EngineRegistrationInput{Phone: phone, ClientProfileID: "synthetic-profile", AppVersion: "2.26.24.77", DeliveryMethod: waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS}
	probe := engine.ProbeAccount(t.Context(), input)
	code := engine.RequestVerificationCode(t.Context(), input)
	if probe.Err != nil || code.Err != nil || store.state.VerificationContext.GetAuthResponse() != "synthetic-server-proof" || store.state.LastCodeResult["auth_response"] != "<redacted>" {
		t.Fatal("request-code runtime did not preserve private continuation")
	}
	registered := engine.SubmitVerificationCode(t.Context(), EngineSubmitInput{EngineRegistrationInput: input, Code: "000000"})
	if registered.Err != nil || registered.Status != waappv1.RegistrationStatus_REGISTRATION_STATUS_REGISTERED {
		t.Fatal("synthetic verification failed")
	}
	for range 3 {
		select {
		case ua := <-observed:
			if !strings.HasPrefix(ua, "WhatsApp/2.26.40.76 ") {
				t.Fatal("runtime used mismatched version or fabricated attestation")
			}
		case <-time.After(time.Second):
			t.Fatal("runtime did not complete a registration stage")
		}
	}
	input.AppVersion = "2.26.41.1"
	if result := engine.RequestVerificationCode(t.Context(), input); result.Err == nil {
		t.Fatal("unsupported build reached the registration endpoint")
	}
	if len(observed) != 0 {
		t.Fatal("unsupported build made a network request")
	}
}
