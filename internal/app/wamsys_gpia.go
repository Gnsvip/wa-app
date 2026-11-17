package app

import (
	"crypto/aes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/curve25519"
)

const nativeGPIAPackageName = "com.whatsapp"

// No device SDK is configured: report the upstream unavailable outcome instead
// of inventing a device's Play Integrity status.
const nativeIntegrityUnavailableCode = 1000

type nativeGPIAMaterial struct {
	Primary       string
	CodeCompact   string
	DeviceCompact string
}

type nativeGPIAJSONField struct {
	Key   string
	Value any
}

func buildNativeGPIAErrorMaterial(input wamsysMaterialInput) (nativeGPIAMaterial, error) {
	sourceDir := nativeGPIASourceDir(input)
	keySource := nativeGPIAKeySource(input.State)
	build, err := androidRegistrationBuild()
	if err != nil {
		return nativeGPIAMaterial{}, err
	}
	material := build.GetApplicationIntegrity()
	errorCode := nativeIntegrityUnavailableCode
	unavailableLibraries := sha256.Sum256([]byte("signal_error"))
	primaryFields := []nativeGPIAJSONField{
		{Key: "sizeInBytes", Value: strconv.FormatUint(material.GetApkSize(), 10)},
		{Key: "packageName", Value: nativeGPIAPackageName},
		{Key: "code", Value: errorCode},
		{Key: "shatr", Value: base64.StdEncoding.EncodeToString(material.GetApkPrefixSha256())},
		{Key: "p", Value: sourceDir},
		{Key: "cert", Value: base64.StdEncoding.EncodeToString(material.GetCertificateSha1())},
		{Key: "sha256", Value: base64.StdEncoding.EncodeToString(material.GetApkSha256())},
	}
	logNativeGPIAPlaintextShape(input, "primary_long", keySource, primaryFields)
	primary, err := encryptNativeGPIAJSON(keySource, primaryFields)
	if err != nil {
		return nativeGPIAMaterial{}, err
	}
	codeCompactFields := []nativeGPIAJSONField{
		{Key: "_ic", Value: errorCode},
	}
	logNativeGPIAPlaintextShape(input, "token_compact", keySource, codeCompactFields)
	codeCompact, err := encryptNativeGPIAJSON(keySource, codeCompactFields)
	if err != nil {
		return nativeGPIAMaterial{}, err
	}
	deviceCompactFields := []nativeGPIAJSONField{
		{Key: "_dh", Value: base64.StdEncoding.EncodeToString(material.GetPrimaryDexSha256())},
		{Key: "_iln", Value: base64.StdEncoding.EncodeToString(unavailableLibraries[:])},
		{Key: "_isb", Value: strconv.FormatUint(material.GetApkSize(), 10)},
		{Key: "_ip", Value: nativeGPIAPackageName},
		{Key: "did", Value: nativeGPIADisplayID(input.State)},
		{Key: "_p", Value: sourceDir},
		{Key: "_ln", Value: base64.StdEncoding.EncodeToString(unavailableLibraries[:])},
		{Key: "_ist", Value: base64.StdEncoding.EncodeToString(material.GetApkPrefixSha256())},
		{Key: "_icr", Value: base64.StdEncoding.EncodeToString(material.GetCertificateSha1())},
		{Key: "_is", Value: base64.StdEncoding.EncodeToString(material.GetApkSha256())},
	}
	logNativeGPIAPlaintextShape(input, "device_compact", keySource, deviceCompactFields)
	deviceCompact, err := encryptNativeGPIAJSON(keySource, deviceCompactFields)
	if err != nil {
		return nativeGPIAMaterial{}, err
	}
	return nativeGPIAMaterial{Primary: primary, CodeCompact: codeCompact, DeviceCompact: deviceCompact}, nil
}

func nativeGPIADisplayID(state nativeState) string {
	profile := normalizeNativePhoneProfile(state.Profile, "")
	return firstNonEmpty(profile.BuildDisplayID, defaultNativeDeviceModel().BuildDisplayID)
}

func nativeGPIASourceDir(input wamsysMaterialInput) string {
	return nativeStableGPIASourceDir(input.State)
}

func nativeStableGPIASourceDir(state nativeState) string {
	first := nativeStableInstallToken(state, "source-dir-prefix")
	second := nativeStableInstallToken(state, "source-dir-package")
	return "/data/app/~~" + first + "==/com.whatsapp-" + second + "==/base.apk"
}

func nativeStableInstallToken(state nativeState, label string) string {
	sum := sha256.Sum256([]byte(nativeStableRuntimeSeed(state, label)))
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}

func nativeGPIAKeySource(state nativeState) string {
	if private, err := state.ChatStatic.privateBytes(); err == nil && len(private) == curve25519.ScalarSize {
		if public, err := curve25519.X25519(private, curve25519.Basepoint); err == nil {
			return base64.StdEncoding.EncodeToString(public)
		}
	}
	for _, candidate := range []string{state.AuthKey, state.ChatStatic.Public} {
		public, err := decodeB64Any(candidate)
		if err == nil && len(public) == curve25519.PointSize {
			return base64.StdEncoding.EncodeToString(public)
		}
	}
	return "default"
}

func encryptNativeGPIAJSON(keySource string, fields []nativeGPIAJSONField) (string, error) {
	plaintext, err := renderNativeGPIAJSONObject(fields)
	if err != nil {
		return "", err
	}
	return encryptNativeGPIAData(keySource, plaintext)
}

func encryptNativeGPIAData(keySource string, plaintext []byte) (string, error) {
	key := sha256.Sum256([]byte(keySource))
	iv := randomBytes(aes.BlockSize)
	ciphertext, err := aesCBCPKCS7Encrypt(plaintext, key[:], iv)
	if err != nil {
		return "", err
	}
	out := make([]byte, 0, len(iv)+len(ciphertext))
	out = append(out, iv...)
	out = append(out, ciphertext...)
	return base64.StdEncoding.EncodeToString(out), nil
}

func renderNativeGPIAJSONObject(fields []nativeGPIAJSONField) ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, field := range fields {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(renderNativeGPIAJSONString(field.Key))
		b.WriteByte(':')
		value, err := renderNativeGPIAJSONValue(field.Value)
		if err != nil {
			return nil, err
		}
		b.Write(value)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

func renderNativeGPIAJSONValue(value any) ([]byte, error) {
	switch v := value.(type) {
	case string:
		return renderNativeGPIAJSONString(v), nil
	case int:
		return []byte(strconv.Itoa(v)), nil
	case int64:
		return []byte(strconv.FormatInt(v, 10)), nil
	case bool:
		return []byte(strconv.FormatBool(v)), nil
	case nil:
		return []byte("null"), nil
	default:
		return nil, fmt.Errorf("unsupported native GPIA JSON value type %T", value)
	}
}

func renderNativeGPIAJSONString(value string) []byte {
	var b strings.Builder
	b.Grow(len(value) + 2)
	b.WriteByte('"')
	for _, char := range value {
		switch char {
		case '"', '\\', '/':
			b.WriteByte('\\')
			b.WriteRune(char)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if char <= 0x1f {
				_, _ = fmt.Fprintf(&b, `\u%04x`, char)
				continue
			}
			b.WriteRune(char)
		}
	}
	b.WriteByte('"')
	return []byte(b.String())
}
