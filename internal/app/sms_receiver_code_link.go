package app

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

const (
	smsReceiverCodeLinkPath          = "/api/wa/actions/sms-receiver/code-link"
	smsReceiverCodeLinkDefaultWait   = 180 * time.Second
	smsReceiverCodeLinkMaxWait       = 10 * time.Minute
	smsReceiverCodeLinkMinInterval   = 2 * time.Second
	smsReceiverCodeLinkStateTTL      = 100 * 365 * 24 * time.Hour
	smsReceiverCodeLinkAAD           = "wa-app:sms-receiver-code-link:v1"
	smsReceiverCodeLinkStatePrefix   = "sms-receiver-code-link:"
	smsReceiverCodeLinkOwnerPrefix   = "sms-receiver-code-link-owner:"
	smsReceiverCodeLinkSessionPrefix = "sms-receiver-code-link-session:"
	smsReceiverCodeLinkSessionTTL    = 5 * time.Minute
	smsReceiverCodeLinkTokenLength   = 64
)

type smsReceiverCodeLinkClaims struct {
	WAAccountID     string `json:"wa_account_id,omitempty"`
	Provider        string `json:"provider"`
	ID              string `json:"id"`
	Phone           string `json:"phone"`
	Country         string `json:"country,omitempty"`
	Price           string `json:"price,omitempty"`
	Service         string `json:"service,omitempty"`
	ProxyMode       string `json:"proxy_mode,omitempty"`
	CreatedAtUnix   int64  `json:"created_at_unix"`
	ExpiresAtUnix   int64  `json:"expires_at_unix"`
	TimeoutSeconds  int64  `json:"timeout_seconds"`
	IntervalSeconds int64  `json:"interval_seconds"`
	MarkReady       bool   `json:"mark_ready"`
}

type smsReceiverCodeLinkSession struct {
	Token                string `json:"token"`
	WAAccountID          string `json:"wa_account_id"`
	BaselineOTPMessageID string `json:"baseline_otp_message_id,omitempty"`
	BaselineReceivedUnix int64  `json:"baseline_received_unix,omitempty"`
	CreatedAtUnix        int64  `json:"created_at_unix"`
	ExpiresAtUnix        int64  `json:"expires_at_unix"`
	Consumed             bool   `json:"consumed"`
}

func newSMSReceiverCodeLinkSecret() []byte {
	key := make([]byte, 32)
	if _, err := cryptorand.Read(key); err == nil {
		return key
	}
	sum := sha256.Sum256([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	return sum[:]
}

func (s *Server) smsReceiverCodeLinkKey() []byte {
	if s == nil {
		return nil
	}
	if s.smsReceiver != nil {
		if token := strings.TrimSpace(s.smsReceiver.apiToken); token != "" {
			sum := sha256.Sum256([]byte("wa-app:sms-receiver-code-link:" + token))
			return sum[:]
		}
	}
	if len(s.smsReceiverCodeKey) > 0 {
		sum := sha256.Sum256(s.smsReceiverCodeKey)
		return sum[:]
	}
	return nil
}

func (g *actionGateway) smsReceiverCodeLink(ctx context.Context, order SMSReceiverOrder, payload map[string]any, request map[string]any) (string, string, error) {
	if g == nil || g.server == nil {
		return "", "", NewError(waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, "SMS code link signer is not available", false)
	}
	now := g.server.clock.Now().UTC()
	timeout := smsReceiverCodeLinkDuration(request, "timeoutSeconds", "timeout_seconds", "timeout", smsReceiverCodeLinkDefaultWait, smsReceiverCodeLinkMaxWait)
	interval := smsReceiverCodeLinkDuration(request, "intervalSeconds", "interval_seconds", "pollIntervalSeconds", 5*time.Second, 30*time.Second)
	if interval < smsReceiverCodeLinkMinInterval {
		interval = smsReceiverCodeLinkMinInterval
	}
	claims := smsReceiverCodeLinkClaims{
		Provider:        order.Provider,
		ID:              order.ID,
		Phone:           order.Phone,
		Country:         order.Country,
		Price:           order.Price,
		Service:         order.Service,
		ProxyMode:       order.ProxyMode,
		CreatedAtUnix:   now.Unix(),
		TimeoutSeconds:  int64(timeout / time.Second),
		IntervalSeconds: int64(interval / time.Second),
		MarkReady:       true,
	}
	token, err := g.saveSMSReceiverCodeLinkClaims(ctx, claims, smsReceiverCodeLinkOwnerKey(payload, order))
	if err != nil {
		return "", "", err
	}
	link := smsReceiverCodeLinkURL(payload, token)
	source := objectField(payload, "sms_receiver")
	displayPhone := firstNonEmpty(textField(source, "phone"), textField(payload, "phone"), order.Phone)
	return link, displayPhone + "----" + link, nil
}

func (g *actionGateway) createWAAccountCodeLink(ctx context.Context, payload map[string]any) (map[string]any, error) {
	if g == nil || g.server == nil || g.server.store == nil {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, "WA account store is not configured", false)
	}
	source := objectField(payload, "sms_receiver")
	accountID := firstNonEmpty(
		textField(source, "wa_account_id"),
		textField(source, "waAccountId"),
		textField(payload, "wa_account_id"),
		textField(payload, "waAccountId"),
	)
	account, err := g.server.getWAAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	phone := strings.TrimSpace(account.GetPhone().GetE164Number())
	if phone == "" {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "WA account phone is required", false)
	}
	now := g.server.clock.Now().UTC()
	timeout := smsReceiverCodeLinkDuration(source, "timeoutSeconds", "timeout_seconds", "timeout", smsReceiverCodeLinkDefaultWait, smsReceiverCodeLinkMaxWait)
	interval := smsReceiverCodeLinkDuration(source, "intervalSeconds", "interval_seconds", "pollIntervalSeconds", 5*time.Second, 30*time.Second)
	if interval < smsReceiverCodeLinkMinInterval {
		interval = smsReceiverCodeLinkMinInterval
	}
	claims := smsReceiverCodeLinkClaims{
		WAAccountID:     accountID,
		Phone:           phone,
		CreatedAtUnix:   now.Unix(),
		TimeoutSeconds:  int64(timeout / time.Second),
		IntervalSeconds: int64(interval / time.Second),
	}
	token, err := g.saveSMSReceiverCodeLinkClaims(ctx, claims, "wa-account:"+accountID)
	if err != nil {
		return nil, err
	}
	return waAccountCodeLinkResponse(payload, token, phone), nil
}

func (g *actionGateway) getWAAccountCodeLink(ctx context.Context, payload map[string]any) (map[string]any, error) {
	if g == nil || g.server == nil || g.server.runtime == nil {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, "SMS code link state store is not configured", false)
	}
	source := objectField(payload, "sms_receiver")
	accountID := firstNonEmpty(
		textField(source, "wa_account_id"),
		textField(source, "waAccountId"),
		textField(payload, "wa_account_id"),
		textField(payload, "waAccountId"),
	)
	account, err := g.server.getWAAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	phone := strings.TrimSpace(account.GetPhone().GetE164Number())
	ownerRef := smsReceiverCodeLinkOwnerRef("wa-account:" + accountID)
	raw, err := g.server.runtime.GetTransientState(ctx, ownerRef)
	if err != nil {
		return map[string]any{"success": true, "generated": false}, nil
	}
	token := strings.TrimSpace(string(raw))
	claims, err := g.decodeSMSReceiverCodeLinkClaims(ctx, token)
	if err != nil || claims.WAAccountID != accountID {
		return map[string]any{"success": true, "generated": false}, nil
	}
	return waAccountCodeLinkResponse(payload, token, phone), nil
}

func waAccountCodeLinkResponse(payload map[string]any, token string, phone string) map[string]any {
	link := waAccountCodeLinkURL(payload, token)
	line := phone + "----" + link
	order := SMSReceiverOrder{Phone: phone, SMSURL: link, SMSLine: line}
	return map[string]any{
		"success":   true,
		"generated": true,
		"order":     smsReceiverOrderMap(order),
		"sms_url":   link,
		"sms_line":  line,
	}
}

func smsReceiverCodeLinkURL(payload map[string]any, token string) string {
	return smsReceiverCodeLinkURLWithMode(payload, token, "1", "text")
}

func waAccountCodeLinkURL(payload map[string]any, token string) string {
	return smsReceiverCodeLinkURLWithMode(payload, token, "0", "json")
}

func smsReceiverCodeLinkURLWithMode(payload map[string]any, token string, wait string, format string) string {
	values := url.Values{}
	values.Set("token", token)
	values.Set("wait", wait)
	values.Set("format", format)
	link := smsReceiverCodeLinkPath + "?" + values.Encode()
	if origin := strings.TrimRight(textField(payload, "_request_origin"), "/"); origin != "" {
		link = origin + link
	}
	return link
}

func smsReceiverCodeLinkDuration(data map[string]any, camel string, snake string, legacy string, fallback time.Duration, maxValue time.Duration) time.Duration {
	seconds := firstPositiveInt64(numberField(data, camel), numberField(data, snake), numberField(data, legacy))
	if seconds <= 0 {
		return fallback
	}
	value := time.Duration(seconds) * time.Second
	if maxValue > 0 && value > maxValue {
		return maxValue
	}
	return value
}

func (g *actionGateway) saveSMSReceiverCodeLinkClaims(ctx context.Context, claims smsReceiverCodeLinkClaims, ownerKey string) (string, error) {
	if g == nil || g.server == nil || g.server.runtime == nil {
		return "", NewError(waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, "SMS code link state store is not configured", false)
	}
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	for attempt := 0; attempt < 3; attempt++ {
		token, err := newSMSReceiverCodeLinkToken()
		if err != nil {
			return "", err
		}
		if err := g.server.runtime.SaveTransientState(ctx, smsReceiverCodeLinkStateRef(token), raw, smsReceiverCodeLinkStateTTL); err != nil {
			return "", err
		}
		if ownerKey != "" {
			ownerRef := smsReceiverCodeLinkOwnerRef(ownerKey)
			oldToken := ""
			if oldRaw, err := g.server.runtime.GetTransientState(ctx, ownerRef); err == nil {
				oldToken = strings.TrimSpace(string(oldRaw))
			}
			if err := g.server.runtime.SaveTransientState(ctx, ownerRef, []byte(token), smsReceiverCodeLinkStateTTL); err != nil {
				_ = g.server.runtime.DeleteTransientState(ctx, smsReceiverCodeLinkStateRef(token))
				return "", err
			}
			if oldToken != "" && oldToken != token && validSMSReceiverCodeLinkToken(oldToken) {
				_ = g.server.runtime.DeleteTransientState(ctx, smsReceiverCodeLinkStateRef(oldToken))
			}
		}
		return token, nil
	}
	return "", NewError(waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, "SMS code link token generation failed", true)
}

func newSMSReceiverCodeLinkToken() (string, error) {
	var raw [32]byte
	if _, err := cryptorand.Read(raw[:]); err != nil {
		return "", err
	}
	var out [smsReceiverCodeLinkTokenLength]byte
	hex.Encode(out[:], raw[:])
	return string(out[:]), nil
}

func smsReceiverCodeLinkStateRef(token string) string {
	return smsReceiverCodeLinkStatePrefix + strings.TrimSpace(token)
}

func smsReceiverCodeLinkOwnerRef(ownerKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(ownerKey)))
	return smsReceiverCodeLinkOwnerPrefix + hex.EncodeToString(sum[:])
}

func smsReceiverCodeLinkOwnerKey(payload map[string]any, order SMSReceiverOrder) string {
	source := objectField(payload, "sms_receiver")
	if accountID := firstNonEmpty(textField(source, "wa_account_id"), textField(source, "waAccountId"), textField(payload, "wa_account_id"), textField(payload, "waAccountId")); accountID != "" {
		return "wa-account:" + accountID
	}
	if order.Phone != "" {
		return "phone:" + order.Phone
	}
	return ""
}

func validSMSReceiverCodeLinkToken(token string) bool {
	if len(token) != smsReceiverCodeLinkTokenLength {
		return false
	}
	for _, ch := range token {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') && (ch < 'A' || ch > 'F') {
			return false
		}
	}
	return true
}

func (g *actionGateway) decodeSMSReceiverCodeLinkClaims(ctx context.Context, token string) (smsReceiverCodeLinkClaims, error) {
	token = strings.TrimSpace(token)
	if validSMSReceiverCodeLinkToken(token) {
		if g == nil || g.server == nil || g.server.runtime == nil {
			return smsReceiverCodeLinkClaims{}, errors.New("SMS code link state store is not configured")
		}
		raw, err := g.server.runtime.GetTransientState(ctx, smsReceiverCodeLinkStateRef(token))
		if err != nil {
			return smsReceiverCodeLinkClaims{}, errors.New("invalid or expired SMS code link token")
		}
		var claims smsReceiverCodeLinkClaims
		if err := json.Unmarshal(raw, &claims); err != nil {
			return smsReceiverCodeLinkClaims{}, errors.New("invalid SMS code link token")
		}
		if claims.WAAccountID == "" && (claims.Provider == "" || claims.ID == "") {
			return smsReceiverCodeLinkClaims{}, errors.New("invalid SMS code link token")
		}
		return claims, nil
	}
	return g.decodeLegacySMSReceiverCodeLinkClaims(token)
}

func (g *actionGateway) decodeLegacySMSReceiverCodeLinkClaims(token string) (smsReceiverCodeLinkClaims, error) {
	key := g.server.smsReceiverCodeLinkKey()
	if len(key) == 0 {
		return smsReceiverCodeLinkClaims{}, errors.New("SMS code link signer is not configured")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(token))
	if err != nil {
		return smsReceiverCodeLinkClaims{}, errors.New("invalid SMS code link token")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return smsReceiverCodeLinkClaims{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return smsReceiverCodeLinkClaims{}, err
	}
	if len(raw) <= gcm.NonceSize() {
		return smsReceiverCodeLinkClaims{}, errors.New("invalid SMS code link token")
	}
	nonce := raw[:gcm.NonceSize()]
	ciphertext := raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, []byte(smsReceiverCodeLinkAAD))
	if err != nil {
		return smsReceiverCodeLinkClaims{}, errors.New("invalid SMS code link token")
	}
	var claims smsReceiverCodeLinkClaims
	if err := json.Unmarshal(plain, &claims); err != nil {
		return smsReceiverCodeLinkClaims{}, errors.New("invalid SMS code link token")
	}
	if claims.WAAccountID == "" && (claims.Provider == "" || claims.ID == "") {
		return smsReceiverCodeLinkClaims{}, errors.New("invalid SMS code link token")
	}
	return claims, nil
}

func (g *actionGateway) handleSMSReceiverCodeLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeSMSReceiverCodeLinkError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if g == nil || g.server == nil {
		writeSMSReceiverCodeLinkError(w, r, http.StatusServiceUnavailable, "WA service is not configured")
		return
	}
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	claims, err := g.decodeSMSReceiverCodeLinkClaims(r.Context(), token)
	if err != nil {
		writeSMSReceiverCodeLinkError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if claims.ExpiresAtUnix > 0 && g.server.clock.Now().After(time.Unix(claims.ExpiresAtUnix, 0)) {
		writeSMSReceiverCodeLinkError(w, r, http.StatusGone, "SMS code link expired")
		return
	}
	if claims.WAAccountID != "" && smsReceiverCodeLinkBoolQuery(r, "prepare") {
		result, err := g.prepareWAAccountCodeLink(r.Context(), token, claims, r)
		if err != nil {
			writeSMSReceiverCodeLinkError(w, r, http.StatusBadGateway, safeInternalErrorMessage(err))
			return
		}
		writeActionJSON(w, http.StatusOK, result)
		return
	}
	order := SMSReceiverOrder{
		Provider:  claims.Provider,
		ID:        claims.ID,
		Phone:     claims.Phone,
		Country:   claims.Country,
		Price:     claims.Price,
		Service:   claims.Service,
		ProxyMode: claims.ProxyMode,
	}
	wait := smsReceiverCodeLinkBoolQuery(r, "wait")
	timeout := smsReceiverCodeLinkQueryDuration(r, "timeout", time.Duration(claims.TimeoutSeconds)*time.Second, smsReceiverCodeLinkMaxWait)
	interval := smsReceiverCodeLinkQueryDuration(r, "interval", time.Duration(claims.IntervalSeconds)*time.Second, 30*time.Second)
	if interval < smsReceiverCodeLinkMinInterval {
		interval = smsReceiverCodeLinkMinInterval
	}
	if claims.WAAccountID != "" {
		if requestID := strings.TrimSpace(r.URL.Query().Get("request_id")); requestID != "" {
			result, err := g.waAccountCodeFromSession(r.Context(), token, requestID, claims, wait, timeout, interval)
			if err != nil {
				writeSMSReceiverCodeLinkError(w, r, http.StatusBadGateway, safeInternalErrorMessage(err))
				return
			}
			writeWAAccountCodeLinkSessionResult(w, r, order, result)
			return
		}
		code, received, err := g.waAccountCodeFromLink(r.Context(), claims, wait, timeout, interval)
		if err != nil {
			writeSMSReceiverCodeLinkError(w, r, http.StatusBadGateway, safeInternalErrorMessage(err))
			return
		}
		writeSMSReceiverCodeLinkResult(w, r, order, code, received)
		return
	}
	if g.server.smsReceiver == nil || !g.server.smsReceiver.Configured() {
		writeSMSReceiverCodeLinkError(w, r, http.StatusServiceUnavailable, "SMS receiver gateway is not configured")
		return
	}
	code, received, err := g.smsReceiverCodeFromLink(r.Context(), order, wait, claims.MarkReady, timeout, interval)
	if err != nil {
		if wait && smsReceiverCodeLinkTimeoutError(err) {
			writeSMSReceiverCodeLinkResult(w, r, order, "", false)
			return
		}
		writeSMSReceiverCodeLinkError(w, r, http.StatusBadGateway, safeInternalErrorMessage(err))
		return
	}
	writeSMSReceiverCodeLinkResult(w, r, order, code, received)
}

func (g *actionGateway) prepareWAAccountCodeLink(ctx context.Context, token string, claims smsReceiverCodeLinkClaims, r *http.Request) (map[string]any, error) {
	if g == nil || g.server == nil || g.server.runtime == nil || g.server.store == nil {
		return nil, errors.New("WA code link session store is not configured")
	}
	items, _, err := g.server.store.ListAccountOTPMessages(ctx, claims.WAAccountID, "", 1, false)
	if err != nil {
		return nil, err
	}
	now := g.server.clock.Now().UTC()
	session := smsReceiverCodeLinkSession{
		Token:         token,
		WAAccountID:   claims.WAAccountID,
		CreatedAtUnix: now.Unix(),
		ExpiresAtUnix: now.Add(smsReceiverCodeLinkSessionTTL).Unix(),
	}
	if len(items) > 0 && items[0] != nil {
		session.BaselineOTPMessageID = items[0].GetOtpMessageId()
		if receivedAt := items[0].GetReceivedAt(); receivedAt != nil && receivedAt.IsValid() {
			session.BaselineReceivedUnix = receivedAt.AsTime().UnixNano()
		}
	}
	requestID, err := newSMSReceiverCodeLinkToken()
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(session)
	if err != nil {
		return nil, err
	}
	if err := g.server.runtime.SaveTransientState(ctx, smsReceiverCodeLinkSessionRef(requestID), raw, smsReceiverCodeLinkSessionTTL); err != nil {
		return nil, err
	}
	phone := claims.Phone
	pollURL := waAccountCodeLinkSessionURL(r, token, requestID)
	return map[string]any{
		"success":    true,
		"status":     "prepared",
		"request_id": requestID,
		"poll_url":   pollURL,
		"phone":      phone,
		"code":       "",
	}, nil
}

func smsReceiverCodeLinkSessionRef(requestID string) string {
	return smsReceiverCodeLinkSessionPrefix + strings.TrimSpace(requestID)
}

func waAccountCodeLinkSessionURL(r *http.Request, token string, requestID string) string {
	values := url.Values{}
	values.Set("token", token)
	values.Set("request_id", requestID)
	values.Set("wait", "0")
	values.Set("format", "json")
	link := smsReceiverCodeLinkPath + "?" + values.Encode()
	if origin := smsReceiverRequestOrigin(r); origin != "" {
		return origin + link
	}
	return link
}

type smsReceiverCodeLinkSessionResult struct {
	Code         string
	Received     bool
	OTPMessageID string
	ReceivedAt   time.Time
}

func (g *actionGateway) waAccountCodeFromSession(ctx context.Context, token string, requestID string, claims smsReceiverCodeLinkClaims, wait bool, timeout time.Duration, interval time.Duration) (smsReceiverCodeLinkSessionResult, error) {
	deadline := g.server.clock.Now().Add(timeout)
	for {
		var session smsReceiverCodeLinkSession
		raw, err := g.server.runtime.GetTransientState(ctx, smsReceiverCodeLinkSessionRef(requestID))
		if err != nil || json.Unmarshal(raw, &session) != nil || session.Token != token || session.WAAccountID != claims.WAAccountID {
			return smsReceiverCodeLinkSessionResult{}, errors.New("invalid or expired code link request")
		}
		if session.ExpiresAtUnix > 0 && g.server.clock.Now().After(time.Unix(session.ExpiresAtUnix, 0)) {
			return smsReceiverCodeLinkSessionResult{}, errors.New("code link request expired")
		}
		if session.Consumed {
			return smsReceiverCodeLinkSessionResult{}, nil
		}
		code, received, otpID, receivedAt, err := g.latestWAAccountCodeAfter(ctx, claims.WAAccountID, session.BaselineOTPMessageID, session.BaselineReceivedUnix)
		if err != nil {
			return smsReceiverCodeLinkSessionResult{}, err
		}
		if received {
			session.Consumed = true
			updated, marshalErr := json.Marshal(session)
			if marshalErr != nil {
				return smsReceiverCodeLinkSessionResult{}, marshalErr
			}
			if saveErr := g.server.runtime.SaveTransientState(ctx, smsReceiverCodeLinkSessionRef(requestID), updated, smsReceiverCodeLinkSessionTTL); saveErr != nil {
				return smsReceiverCodeLinkSessionResult{}, saveErr
			}
			return smsReceiverCodeLinkSessionResult{Code: code, Received: true, OTPMessageID: otpID, ReceivedAt: receivedAt}, nil
		}
		if !wait {
			return smsReceiverCodeLinkSessionResult{}, nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return smsReceiverCodeLinkSessionResult{}, nil
		}
		pollDelay := interval
		if pollDelay <= 0 || pollDelay > remaining {
			pollDelay = remaining
		}
		timeoutTimer := time.NewTimer(pollDelay)
		select {
		case <-ctx.Done():
			timeoutTimer.Stop()
			return smsReceiverCodeLinkSessionResult{}, ctx.Err()
		case <-timeoutTimer.C:
			return smsReceiverCodeLinkSessionResult{}, nil
		case <-time.After(interval):
		}
	}
}

func (g *actionGateway) latestWAAccountCodeAfter(ctx context.Context, accountID string, baselineID string, baselineReceivedUnix int64) (string, bool, string, time.Time, error) {
	items, _, err := g.server.store.ListAccountOTPMessages(ctx, accountID, "", 20, true)
	if err != nil {
		return "", false, "", time.Time{}, err
	}
	now := g.server.clock.Now().UTC()
	for _, item := range items {
		if item == nil || item.GetReceivedAt() == nil || !item.GetReceivedAt().IsValid() {
			continue
		}
		receivedAt := item.GetReceivedAt().AsTime().UTC()
		if baselineReceivedUnix > 0 && receivedAt.UnixNano() <= baselineReceivedUnix {
			continue
		}
		if baselineReceivedUnix == 0 && baselineID != "" && item.GetOtpMessageId() == baselineID {
			continue
		}
		if expiresAt := item.GetExpiresAt(); expiresAt != nil && expiresAt.IsValid() && !now.Before(expiresAt.AsTime()) {
			continue
		}
		if code := strings.TrimSpace(item.GetOtp().GetValue()); code != "" {
			return code, true, item.GetOtpMessageId(), receivedAt, nil
		}
	}
	return "", false, "", time.Time{}, nil
}

func writeWAAccountCodeLinkSessionResult(w http.ResponseWriter, r *http.Request, order SMSReceiverOrder, result smsReceiverCodeLinkSessionResult) {
	if smsReceiverCodeLinkTextResponse(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		writeActionJSON(w, http.StatusOK, map[string]any{"success": true, "received": result.Received, "status": map[bool]string{true: "received", false: "waiting"}[result.Received], "code": result.Code, "otp_id": result.OTPMessageID, "received_at": result.ReceivedAt})
		return
	}
	payload := map[string]any{"success": true, "received": result.Received, "status": map[bool]string{true: "received", false: "waiting"}[result.Received], "code": result.Code, "otp_id": result.OTPMessageID, "received_at": result.ReceivedAt, "phone": order.Phone}
	writeActionJSON(w, http.StatusOK, payload)
}

func (g *actionGateway) deleteSMSReceiverCodeLink(ctx context.Context, payload map[string]any) (map[string]any, error) {
	if g == nil || g.server == nil || g.server.runtime == nil {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, "SMS code link state store is not configured", false)
	}
	source := objectField(payload, "sms_receiver")
	token := firstNonEmpty(
		textField(source, "token"),
		textField(payload, "token"),
		smsReceiverCodeLinkTokenFromURL(textField(source, "sms_url")),
		smsReceiverCodeLinkTokenFromURL(textField(source, "smsUrl")),
		smsReceiverCodeLinkTokenFromURL(textField(payload, "sms_url")),
	)
	deleted := false
	if validSMSReceiverCodeLinkToken(token) {
		_ = g.server.runtime.DeleteTransientState(ctx, smsReceiverCodeLinkStateRef(token))
		deleted = true
	}
	ownerKey := smsReceiverCodeLinkOwnerKey(payload, SMSReceiverOrder{Phone: textField(source, "phone")})
	if ownerKey != "" {
		ownerRef := smsReceiverCodeLinkOwnerRef(ownerKey)
		if raw, err := g.server.runtime.GetTransientState(ctx, ownerRef); err == nil {
			oldToken := strings.TrimSpace(string(raw))
			if oldToken != "" && oldToken != token && validSMSReceiverCodeLinkToken(oldToken) {
				_ = g.server.runtime.DeleteTransientState(ctx, smsReceiverCodeLinkStateRef(oldToken))
			}
			deleted = true
		}
		_ = g.server.runtime.DeleteTransientState(ctx, ownerRef)
	}
	return map[string]any{"success": true, "deleted": deleted}, nil
}

func smsReceiverCodeLinkTokenFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Query().Get("token"))
}

func (g *actionGateway) smsReceiverCodeFromLink(ctx context.Context, order SMSReceiverOrder, wait bool, markReady bool, timeout time.Duration, interval time.Duration) (string, bool, error) {
	if wait {
		code, err := g.server.smsReceiver.WaitCodeWithOptions(ctx, order, timeout, interval, markReady, false)
		return code, code != "", err
	}
	if markReady {
		if err := g.server.smsReceiver.Ready(ctx, order); err != nil {
			return "", false, err
		}
	}
	return g.server.smsReceiver.Code(ctx, order)
}

func (g *actionGateway) waAccountCodeFromLink(ctx context.Context, claims smsReceiverCodeLinkClaims, wait bool, timeout time.Duration, interval time.Duration) (string, bool, error) {
	if !wait {
		return g.latestWAAccountCode(ctx, claims)
	}
	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()
	pollTicker := time.NewTicker(interval)
	defer pollTicker.Stop()
	for {
		code, received, err := g.latestWAAccountCode(ctx, claims)
		if err != nil || received {
			return code, received, err
		}
		select {
		case <-ctx.Done():
			return "", false, ctx.Err()
		case <-timeoutTimer.C:
			return "", false, nil
		case <-pollTicker.C:
		}
	}
}

func (g *actionGateway) latestWAAccountCode(ctx context.Context, claims smsReceiverCodeLinkClaims) (string, bool, error) {
	items, _, err := g.server.store.ListAccountOTPMessages(ctx, claims.WAAccountID, "", 20, true)
	if err != nil {
		return "", false, err
	}
	createdAt := time.Unix(claims.CreatedAtUnix, 0).UTC()
	now := g.server.clock.Now().UTC()
	for _, item := range items {
		if item == nil || item.GetReceivedAt() == nil || !item.GetReceivedAt().IsValid() {
			continue
		}
		if item.GetReceivedAt().AsTime().UTC().Before(createdAt) {
			continue
		}
		if expiresAt := item.GetExpiresAt(); expiresAt != nil && expiresAt.IsValid() && !now.Before(expiresAt.AsTime()) {
			continue
		}
		if code := strings.TrimSpace(item.GetOtp().GetValue()); code != "" {
			return code, true, nil
		}
	}
	return "", false, nil
}

func smsReceiverCodeLinkTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "code timeout") || strings.Contains(text, "timed out") || strings.Contains(text, "timeout")
}

func writeSMSReceiverCodeLinkResult(w http.ResponseWriter, r *http.Request, order SMSReceiverOrder, code string, received bool) {
	if smsReceiverCodeLinkTextResponse(r) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if received && strings.TrimSpace(code) != "" {
			_, _ = fmt.Fprintln(w, strings.TrimSpace(code))
			return
		}
		_, _ = fmt.Fprintln(w, "WAITING")
		return
	}
	status := "waiting"
	if received {
		status = "received"
	}
	payload := map[string]any{
		"success":  true,
		"received": received,
		"status":   status,
		"code":     strings.TrimSpace(code),
		"provider": order.Provider,
		"id":       order.ID,
		"phone":    order.Phone,
	}
	writeActionJSON(w, http.StatusOK, payload)
}

func writeSMSReceiverCodeLinkError(w http.ResponseWriter, r *http.Request, status int, message string) {
	if smsReceiverCodeLinkTextResponse(r) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, "ERROR: %s\n", message)
		return
	}
	writeActionJSON(w, status, map[string]any{"success": false, "error_message": message})
}

func smsReceiverCodeLinkTextResponse(r *http.Request) bool {
	q := r.URL.Query()
	return strings.EqualFold(q.Get("format"), "text") || smsReceiverCodeLinkBoolQuery(r, "plain")
}

func smsReceiverCodeLinkBoolQuery(r *http.Request, key string) bool {
	value := strings.ToLower(strings.TrimSpace(r.URL.Query().Get(key)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func smsReceiverCodeLinkQueryDuration(r *http.Request, key string, fallback time.Duration, maxValue time.Duration) time.Duration {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || seconds <= 0 {
		return fallback
	}
	value := time.Duration(seconds) * time.Second
	if maxValue > 0 && value > maxValue {
		return maxValue
	}
	return value
}

func smsReceiverRequestOrigin(r *http.Request) string {
	if r == nil {
		return ""
	}
	scheme := firstForwardedHeader(r.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	if scheme != "http" && scheme != "https" {
		scheme = "http"
	}
	host := firstForwardedHeader(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

func firstForwardedHeader(value string) string {
	if value == "" {
		return ""
	}
	return strings.TrimSpace(strings.Split(value, ",")[0])
}
