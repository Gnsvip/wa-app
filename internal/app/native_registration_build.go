package app

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"sync"

	protocolv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/proto"
)

// Bundle and field layout from pood1e/wa-app at
// 73423e459fd3dabaa0b4bf4453e0e96edee98e3f. Keep all APK material versioned together.
//
//go:embed android-2.26.40.76.pb
var androidRegistrationBuildData []byte

const defaultRegistrationVersionCode = 264007622

var androidRegistrationBuild = sync.OnceValues(func() (*protocolv1.AndroidBuildMaterial, error) {
	build := new(protocolv1.AndroidBuildMaterial)
	if err := proto.Unmarshal(androidRegistrationBuildData, build); err != nil {
		return nil, errors.New("decode Android registration build failed")
	}
	if err := validateAndroidRegistrationBuild(build); err != nil {
		return nil, err
	}
	return build, nil
})

func validateAndroidRegistrationBuild(build *protocolv1.AndroidBuildMaterial) error {
	if build.GetAppVersion() != defaultWAAppVersion || build.GetVersionCode() != defaultRegistrationVersionCode {
		return errors.New("Android registration build version mismatch")
	}
	if len(build.GetTokenHmacKey()) != 64 || len(build.GetClassesDexMd5()) != md5.Size {
		return errors.New("invalid Android registration token material")
	}
	if _, err := x509.ParseCertificate(build.GetSigningCertificate()); err != nil {
		return errors.New("invalid Android signing certificate")
	}
	material := build.GetApplicationIntegrity()
	if material.GetApkSize() == 0 || len(material.GetApkSha256()) != 32 || len(material.GetApkPrefixSha256()) != 32 || len(material.GetPrimaryDexSha256()) != 32 {
		return errors.New("invalid Android APK integrity material")
	}
	digest := sha1.Sum(build.GetSigningCertificate())
	if !bytes.Equal(digest[:], material.GetCertificateSha1()) {
		return errors.New("Android certificate digest mismatch")
	}
	server, err := hex.DecodeString(defaultWASafeServerPublicKeyHex)
	if err != nil || !bytes.Equal(server, build.GetServerPublicKey()) {
		return errors.New("Android registration transport key mismatch")
	}
	return nil
}

// Profiles created by the previous release can use the new bundled build.
// Other requested versions require their own matching material before use.
func registrationAppVersion(version string) (string, error) {
	switch strings.TrimSpace(version) {
	case "", "2.26.24.77", defaultWAAppVersion:
		return defaultWAAppVersion, nil
	default:
		return "", NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "requested registration version has no matching APK material", false)
	}
}

func deriveDefaultRegistrationToken(phone string) string {
	build, err := androidRegistrationBuild()
	if err != nil || phone == "" || len(phone) > 14 || strings.IndexFunc(phone, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return ""
	}
	mac := hmac.New(sha1.New, build.GetTokenHmacKey())
	_, _ = mac.Write(build.GetSigningCertificate())
	_, _ = mac.Write(build.GetClassesDexMd5())
	_, _ = mac.Write([]byte(phone))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func updateRegistrationVerification(state *nativeState, data map[string]any) {
	if state.VerificationContext == nil {
		state.VerificationContext = &protocolv1.AndroidVerificationContext{}
	}
	for key, target := range map[string]*string{
		"login": &state.VerificationContext.Login, "auth_response": &state.VerificationContext.AuthResponse,
		"context": &state.VerificationContext.Context, "advertising_id": &state.VerificationContext.AdvertisingId,
	} {
		if value := jsonString(data[key]); value != "" && value != "<redacted>" {
			*target = value
		}
	}
}

func registrationVerificationValue(state nativeState, key string) string {
	var value string
	switch key {
	case "login":
		value = state.VerificationContext.GetLogin()
	case "auth_response":
		value = state.VerificationContext.GetAuthResponse()
	case "context":
		value = state.VerificationContext.GetContext()
	case "advertising_id":
		value = state.VerificationContext.GetAdvertisingId()
	}
	if value == "" {
		value = jsonString(state.LastCodeResult[key])
	}
	if value == "<redacted>" {
		return ""
	}
	return value
}
