package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/byte-v-forge/wa-app/internal/app"
	"github.com/nyaruka/phonenumbers"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type dashboardHTTP struct {
	staticDir     string
	service       *app.Server
	actionHandler http.Handler
}

func runDashboardHTTP(ctx context.Context, listenAddr, staticDir string, service *app.Server, actionHandler http.Handler, auth dashboardAuthConfig) error {
	if strings.TrimSpace(listenAddr) == "" {
		return nil
	}
	server := &dashboardHTTP{
		staticDir:     firstNonEmpty(staticDir, "/app/dashboard/wa"),
		service:       service,
		actionHandler: actionHandler,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/wa/health", server.handleHealth)
	mux.HandleFunc("/api/wa/runtime-logs", server.handleRuntimeLogs)
	mux.HandleFunc("/api/wa/auto-sms-registration", server.handleAutoSMSRegistration)
	mux.HandleFunc("/api/wa/phone/sms-probe", server.handlePhoneSMSProbe)
	mux.HandleFunc("/api/wa/register", server.handleRegister)
	mux.HandleFunc("/api/wa/login-state-check", server.handleLoginStateCheck)
	mux.HandleFunc("/api/wa/account-settings/2fa/status", server.handleGetTwoFactorAuthStatus)
	mux.HandleFunc("/api/wa/account-settings/2fa", server.handleSetTwoFactorAuthSettings)
	mux.HandleFunc("/api/wa/account-settings/email", server.handleSetAccountEmail)
	mux.HandleFunc("/api/wa/account-settings/email/otp/request", server.handleRequestAccountEmailOtp)
	mux.HandleFunc("/api/wa/account-settings/email/otp/verify", server.handleVerifyAccountEmailOtp)
	mux.HandleFunc("/api/wa/account-settings/profile/name", server.handleSetAccountProfileName)
	mux.HandleFunc("/api/wa/account-settings/profile/picture", server.handleSetAccountProfilePicture)
	mux.HandleFunc("/api/wa/account-settings/profile/picture/remove", server.handleRemoveAccountProfilePicture)
	mux.HandleFunc("/api/wa/accounts/cleanup-pending-registration", server.handleCleanupPendingRegistrationAccounts)
	mux.HandleFunc("/api/wa/accounts/registration-progress", server.handleAccountRegistrationProgress)
	mux.HandleFunc("/api/wa/accounts", server.handleAccounts)
	mux.HandleFunc("/api/wa/accounts/", server.handleAccount)
	mux.HandleFunc("/api/wa/client-profiles", server.handleClientProfiles)
	mux.HandleFunc("/api/wa/account-otp-messages", server.handleAccountOTPMessages)
	mux.HandleFunc("/api/wa/messages/read", server.handleMarkMessagesRead)
	mux.HandleFunc("/api/wa/messages/delete", server.handleDeleteMessages)
	mux.HandleFunc("/api/wa/messages/send", server.handleSendTextMessage)
	mux.HandleFunc("/api/wa/messages", server.handleMessages)
	mux.HandleFunc("/api/wa/latest-contacts", server.handleLatestContacts)
	mux.HandleFunc("/api/wa/contacts/resolve", server.handleResolveContacts)
	mux.HandleFunc("/api/wa/contacts/", server.handleContactResource)
	mux.HandleFunc("/api/wa/contacts", server.handleContacts)
	mux.HandleFunc("/api/wa/long-connections", server.handleLongConnections)
	mux.Handle("/api/wa/actions/", server.actionHandler)
	mux.HandleFunc("/mf/wa/", http.NotFound)
	mux.HandleFunc("/healthz", server.handleHealth)
	mux.Handle("/", standaloneDashboard(server.staticDir))
	httpServer := &http.Server{Addr: listenAddr, Handler: withCORS(withOptionalDashboardAuth(mux, auth)), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	log.Printf("wa-app dashboard BFF listening on %s", listenAddr)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("wa-app dashboard BFF failed: %w", err)
	}
	return nil
}

func (s *dashboardHTTP) handleLongConnections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	q := r.URL.Query()
	resp, err := s.service.GetLongConnectionStatus(r.Context(), &waappv1.GetLongConnectionStatusRequest{
		Context:              &waappv1.RequestContext{RequestId: newRequestID("wa-conn-status")},
		LoginStateId:         q.Get("login_state_id"),
		WaAccountId:          q.Get("wa_account_id"),
		ClientProfileId:      q.Get("client_profile_id"),
		RegisteredIdentityId: q.Get("registered_identity_id"),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "load long connection status failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleCreateAccount(w, r)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	q := r.URL.Query()
	resp, err := s.service.ListWAAccounts(r.Context(), &waappv1.ListWAAccountsRequest{
		Context: &waappv1.RequestContext{RequestId: newRequestID("wa-account-list")},
		Limit:   int32(positiveInt(q.Get("limit"), 100)),
		Cursor:  q.Get("cursor"),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "load WA accounts failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleAccount(w http.ResponseWriter, r *http.Request) {
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	if strings.HasSuffix(r.URL.Path, "/profile-picture") {
		s.handleAccountProfilePicture(w, r)
		return
	}
	accountID, err := url.PathUnescape(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/wa/accounts/"), "/"))
	if err != nil || accountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "wa_account_id is required"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		resp, err := s.service.GetWAAccount(r.Context(), &waappv1.GetWAAccountRequest{Context: &waappv1.RequestContext{RequestId: newRequestID("wa-account-get")}, WaAccountId: accountID})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "load WA account failed"})
			return
		}
		writeProtoJSON(w, http.StatusOK, resp)
	case http.MethodDelete:
		resp, err := s.service.DeleteWAAccount(r.Context(), &waappv1.DeleteWAAccountRequest{Context: &waappv1.RequestContext{RequestId: newRequestID("wa-account-delete")}, WaAccountId: accountID})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "delete WA account failed"})
			return
		}
		writeProtoJSON(w, http.StatusOK, resp)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodDelete)
	}
}

func (s *dashboardHTTP) handleCleanupPendingRegistrationAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	resp, err := s.service.DeletePendingRegistrationWAAccounts(r.Context(), &waappv1.DeletePendingRegistrationWAAccountsRequest{
		Context: &waappv1.RequestContext{RequestId: newRequestID("wa-account-cleanup-pending")},
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cleanup pending WA accounts failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleAccountRegistrationProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	progress, err := s.service.WAAccountRegistrationProgress(r.Context(), r.URL.Query().Get("wa_account_id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, progress)
}

func (s *dashboardHTTP) handleAccountProfilePicture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	accountID, ok := accountIDFromProfilePicturePath(r.URL.Path)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "wa_account_id is required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	picture, err := s.service.GetWAAccountProfilePicture(ctx, accountID)
	if err != nil {
		writeProfilePictureNotFound(w)
		return
	}
	writeProfilePicture(w, picture.ContentType, picture.ProfilePictureID, "private, no-cache", picture.Data)
}

func (s *dashboardHTTP) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	normalized, err := normalizeWorkflowBody(body, "wa-account-create")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	payload := map[string]any{}
	if err := json.Unmarshal(normalized, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must be json"})
		return
	}
	phone := objectField(payload, "phone")
	resp, err := s.service.CreateWAAccount(r.Context(), &waappv1.CreateWAAccountRequest{
		Context: &waappv1.RequestContext{
			RequestId: textField(payload, "request_id"),
		},
		Phone: &waappv1.PhoneTarget{
			E164Number:         textField(phone, "e164_number"),
			CountryCallingCode: textField(phone, "country_calling_code"),
			NationalNumber:     textField(phone, "national_number"),
			CountryIso2:        textField(phone, "country_iso2"),
		},
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "create WA account failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleClientProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	q := r.URL.Query()
	resp, err := s.service.ListClientProfiles(r.Context(), &waappv1.ListClientProfilesRequest{
		Context:     &waappv1.RequestContext{RequestId: newRequestID("wa-profile-list")},
		WaAccountId: q.Get("wa_account_id"),
		Limit:       int32(positiveInt(q.Get("limit"), 20)),
		Cursor:      q.Get("cursor"),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "load WA client profiles failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleAccountOTPMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	q := r.URL.Query()
	resp, err := s.service.ListAccountOtpMessages(r.Context(), &waappv1.ListAccountOtpMessagesRequest{
		Context: &waappv1.RequestContext{
			RequestId: newRequestID("wa-otp-list"),
		},
		WaAccountId:            q.Get("wa_account_id"),
		Limit:                  int32(positiveInt(q.Get("limit"), 20)),
		Cursor:                 q.Get("cursor"),
		IncludeSensitiveValues: queryBool(q.Get("include_sensitive_values"), true),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "load WA OTP history failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	q := r.URL.Query()
	resp, err := s.service.ListAccountMessages(r.Context(), &waappv1.ListAccountMessagesRequest{
		Context:              &waappv1.RequestContext{RequestId: newRequestID("wa-message-list")},
		WaAccountId:          q.Get("wa_account_id"),
		ContactRef:           q.Get("contact_ref"),
		Limit:                int32(positiveInt(q.Get("limit"), 100)),
		Cursor:               q.Get("cursor"),
		IncludeSensitiveText: true,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "load WA messages failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleMarkMessagesRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	payload, ok := readJSONPayload(w, r)
	if !ok {
		return
	}
	resp, err := s.service.MarkAccountMessagesRead(r.Context(), &waappv1.MarkAccountMessagesReadRequest{
		Context:           &waappv1.RequestContext{RequestId: firstNonEmpty(textField(payload, "request_id"), newRequestID("wa-message-read"))},
		WaAccountId:       textField(payload, "wa_account_id"),
		AccountMessageIds: stringListField(payload, "account_message_ids"),
		LocalOnly:         boolField(payload, "local_only"),
		ContactRef:        textField(payload, "contact_ref"),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "mark WA messages read failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleDeleteMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	payload, ok := readJSONPayload(w, r)
	if !ok {
		return
	}
	resp, err := s.service.DeleteAccountMessages(r.Context(), &waappv1.DeleteAccountMessagesRequest{
		Context:           &waappv1.RequestContext{RequestId: firstNonEmpty(textField(payload, "request_id"), newRequestID("wa-message-delete"))},
		WaAccountId:       textField(payload, "wa_account_id"),
		AccountMessageIds: stringListField(payload, "account_message_ids"),
		Mode:              deleteModeField(payload),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "delete WA messages failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleSendTextMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	payload, ok := readJSONPayload(w, r)
	if !ok {
		return
	}
	resp, err := s.service.SendTextMessage(r.Context(), &waappv1.SendTextMessageRequest{
		Context:         &waappv1.RequestContext{RequestId: firstNonEmpty(textField(payload, "request_id"), newRequestID("wa-message-send"))},
		WaAccountId:     textField(payload, "wa_account_id"),
		ContactRef:      textField(payload, "contact_ref"),
		Text:            &waappv1.SensitiveText{Value: textField(payload, "text")},
		ClientMessageId: textField(payload, "client_message_id"),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "send WA text message failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleContacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	q := r.URL.Query()
	resp, err := s.service.ListWAContacts(r.Context(), &waappv1.ListWAContactsRequest{
		Context:     &waappv1.RequestContext{RequestId: newRequestID("wa-contact-list")},
		WaAccountId: q.Get("wa_account_id"),
		Limit:       int32(positiveInt(q.Get("limit"), 500)),
		Cursor:      q.Get("cursor"),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "load WA contacts failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleLatestContacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	q := r.URL.Query()
	window, since, unreadOnly := latestContactWindow(q.Get("window"), time.Now().UTC())
	limit := positiveInt(q.Get("limit"), 80)
	if limit > 200 {
		limit = 200
	}
	includeUnread := queryBool(q.Get("include_unread"), true)
	if unreadOnly {
		includeUnread = true
	}
	rows, err := s.service.ListLatestWAContacts(r.Context(), app.LatestWAContactFilter{
		Since:         since,
		IncludeUnread: includeUnread,
		UnreadOnly:    unreadOnly,
		SystemOnly:    queryBool(q.Get("system_only"), false),
		Limit:         limit,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "load latest WA contacts failed"})
		return
	}
	resp, err := latestContactsResponse(rows, window, since, includeUnread, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "encode latest WA contacts failed"})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleContactResource(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/profile-picture") {
		s.handleContactProfilePicture(w, r)
		return
	}
	if r.Method == http.MethodDelete {
		s.handleDeleteContact(w, r)
		return
	}
	http.NotFound(w, r)
}

type dashboardLatestContactRow struct {
	Account json.RawMessage `json:"account"`
	Contact json.RawMessage `json:"contact"`
}

type dashboardLatestContactsResponse struct {
	Rows          []dashboardLatestContactRow `json:"rows"`
	Window        string                      `json:"window"`
	Since         string                      `json:"since,omitempty"`
	IncludeUnread bool                        `json:"include_unread"`
	Limit         int                         `json:"limit"`
}

func latestContactsResponse(rows []app.LatestWAContact, window string, since time.Time, includeUnread bool, limit int) (dashboardLatestContactsResponse, error) {
	opts := protojson.MarshalOptions{UseProtoNames: true}
	out := dashboardLatestContactsResponse{Rows: make([]dashboardLatestContactRow, 0, len(rows)), Window: window, IncludeUnread: includeUnread, Limit: limit}
	if !since.IsZero() {
		out.Since = since.UTC().Format(time.RFC3339Nano)
	}
	for _, row := range rows {
		accountJSON, err := opts.Marshal(row.Account)
		if err != nil {
			return out, err
		}
		contactJSON, err := opts.Marshal(row.Contact)
		if err != nil {
			return out, err
		}
		out.Rows = append(out.Rows, dashboardLatestContactRow{Account: json.RawMessage(accountJSON), Contact: json.RawMessage(contactJSON)})
	}
	return out, nil
}

func latestContactWindow(value string, now time.Time) (string, time.Time, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "unread", "未读":
		return "unread", time.Time{}, true
	case "5m", "5min", "5", "5分钟":
		return "5m", now.Add(-5 * time.Minute), false
	case "30m", "30min", "30", "30分钟":
		return "30m", now.Add(-30 * time.Minute), false
	case "1h", "60m", "60", "1小时", "":
		return "1h", now.Add(-time.Hour), false
	default:
		return "1h", now.Add(-time.Hour), false
	}
}

func (s *dashboardHTTP) handleDeleteContact(w http.ResponseWriter, r *http.Request) {
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	contactID, ok := contactIDFromContactPath(r.URL.Path)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "contact id is required"})
		return
	}
	accountID := r.URL.Query().Get("wa_account_id")
	if accountID == "" && r.ContentLength != 0 {
		payload, ok := readJSONPayload(w, r)
		if !ok {
			return
		}
		accountID = textField(payload, "wa_account_id")
	}
	resp, err := s.service.DeleteWAContact(r.Context(), &waappv1.DeleteWAContactRequest{
		Context:     &waappv1.RequestContext{RequestId: newRequestID("wa-contact-delete")},
		WaAccountId: accountID,
		ContactRef:  contactID,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "delete WA contact failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleContactProfilePicture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	contactID, ok := contactIDFromProfilePicturePath(r.URL.Path)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "contact id is required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	picture, err := s.service.GetWAContactProfilePicture(ctx, contactID)
	if err != nil {
		writeProfilePictureNotFound(w)
		return
	}
	writeProfilePicture(w, picture.ContentType, picture.ProfilePictureID, "private, max-age=3600", picture.Data)
}

func writeProfilePictureNotFound(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, max-age=30")
	w.WriteHeader(http.StatusNotFound)
}

func writeProfilePicture(w http.ResponseWriter, contentType string, etagValue string, cacheControl string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", cacheControl)
	if etag := safeHTTPETag(etagValue); etag != "" {
		w.Header().Set("ETag", etag)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func accountIDFromProfilePicturePath(path string) (string, bool) {
	value := strings.TrimSuffix(strings.TrimPrefix(path, "/api/wa/accounts/"), "/profile-picture")
	return parseContactIDPathValue(value)
}

func contactIDFromProfilePicturePath(path string) (string, bool) {
	value := strings.TrimSuffix(strings.TrimPrefix(path, "/api/wa/contacts/"), "/profile-picture")
	return parseContactIDPathValue(value)
}

func contactIDFromContactPath(path string) (string, bool) {
	value := strings.TrimPrefix(path, "/api/wa/contacts/")
	return parseContactIDPathValue(value)
}

func parseContactIDPathValue(value string) (string, bool) {
	value = strings.Trim(value, "/")
	if value == "" || strings.Contains(value, "/") {
		return "", false
	}
	contactID, err := url.PathUnescape(value)
	if err != nil || strings.TrimSpace(contactID) == "" {
		return "", false
	}
	return contactID, true
}

func safeHTTPETag(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == ':' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return `"` + b.String() + `"`
}

func (s *dashboardHTTP) handleResolveContacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	req := &waappv1.ResolveWAContactsRequest{}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if len(bytes.TrimSpace(body)) > 0 {
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(body, req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must be a ResolveWAContactsRequest JSON object"})
			return
		}
	}
	q := r.URL.Query()
	if req.WaAccountId == "" {
		req.WaAccountId = q.Get("wa_account_id")
	}
	if req.Limit == 0 {
		req.Limit = int32(positiveInt(q.Get("limit"), 50))
	}
	if req.Context == nil {
		req.Context = &waappv1.RequestContext{RequestId: newRequestID("wa-contact-resolve")}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	resp, err := s.service.ResolveWAContacts(ctx, req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "resolve WA contacts failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func newWAActionHandler(service *app.Server) http.Handler {
	return app.NewActionGateway(service)
}

func (s *dashboardHTTP) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"workflows": []map[string]string{
			{"key": "register-native", "label": "WA 原生注册流程", "webhook_path": "/api/wa/register"},
		},
	})
}

func (s *dashboardHTTP) handleRuntimeLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	q := r.URL.Query()
	limit := positiveInt(q.Get("limit"), 100)
	if limit > 300 {
		limit = 300
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"logs": s.service.RuntimeLogs(q.Get("after_id"), limit),
		"now":  time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (s *dashboardHTTP) handleAutoSMSRegistration(w http.ResponseWriter, r *http.Request) {
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"task": s.service.AutoSMSRegistrationStatus()})
	case http.MethodPost:
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		payload := map[string]any{}
		if len(strings.TrimSpace(string(body))) > 0 {
			if err := json.Unmarshal(body, &payload); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must be json"})
				return
			}
		}
		task, err := s.service.StartAutoSMSRegistration(r.Context(), app.AutoSMSRegistrationInput{
			SMSReceiver:    objectField(payload, "sms_receiver"),
			TimeoutSeconds: positiveInt(textField(payload, "timeout_seconds"), 180),
			TargetCount:    nonNegativeInt(textField(payload, "target_count"), 0),
		})
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"success": false, "error_message": err.Error(), "task": s.service.AutoSMSRegistrationStatus()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "task": task})
	case http.MethodDelete:
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "task": s.service.StopAutoSMSRegistration(r.Context())})
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost+", "+http.MethodDelete)
	}
}

func (s *dashboardHTTP) handlePhoneSMSProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	normalized, err := normalizeWorkflowBody(body, "wa-phone-sms-probe")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	payload := map[string]any{}
	if err := json.Unmarshal(normalized, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must be json"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 65*time.Second)
	defer cancel()
	result, err := s.service.ProbeNumberSMS(ctx, payload)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "probe WA phone failed"})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *dashboardHTTP) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	normalized, err := normalizeWorkflowBody(body, "wa-register")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	payload := map[string]any{}
	if err := json.Unmarshal(normalized, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must be json"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	result, err := s.service.StartRegistration(ctx, payload)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "status": "REGISTRATION_START_FAILED", "error_message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *dashboardHTTP) handleLoginStateCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	payload := map[string]any{}
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := json.Unmarshal(body, &payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must be json"})
			return
		}
	}
	payload["request_id"] = firstNonEmpty(textField(payload, "request_id"), newRequestID("wa-req"))
	payload["job_id"] = firstNonEmpty(textField(payload, "job_id"), newRequestID("wa-login-state-check"))
	encoded, err := json.Marshal(payload)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "build login-state check request failed"})
		return
	}
	r.URL.Path = "/api/wa/actions/registration/check-login-state"
	r.Body = io.NopCloser(bytes.NewReader(encoded))
	r.ContentLength = int64(len(encoded))
	r.Header.Set("Content-Type", "application/json")
	s.actionHandler.ServeHTTP(w, r)
}

var nonDigits = regexp.MustCompile(`\D+`)

func normalizeWorkflowBody(body []byte, workflowPath string) ([]byte, error) {
	payload := map[string]any{}
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("request body must be json")
		}
	}
	phone, err := normalizePhonePayload(payload)
	if err != nil {
		return nil, err
	}
	payload["request_id"] = firstNonEmpty(textField(payload, "request_id"), newRequestID("wa-req"))
	payload["job_id"] = firstNonEmpty(textField(payload, "job_id"), newRequestID(workflowJobPrefix(workflowPath)))
	payload["country_region"] = phone.countryISO2
	payload["country_code"] = phone.callingCode
	payload["country_calling_code"] = phone.callingCode
	payload["country_iso2"] = phone.countryISO2
	payload["region"] = phone.countryISO2
	payload["phone"] = map[string]any{"e164_number": phone.e164, "country_calling_code": phone.callingCode, "national_number": phone.nationalNumber, "country_iso2": phone.countryISO2}
	return json.Marshal(payload)
}

func workflowJobPrefix(path string) string {
	if strings.Contains(path, "register") {
		return "wa-register"
	}
	return "wa-phone-sms-probe"
}

type normalizedPhone struct {
	e164           string
	callingCode    string
	nationalNumber string
	countryISO2    string
}

const phoneNotPossibleMessage = "手机号位数不符合国家规则，请检查国家拨号码和手机号。"

func normalizePhonePayload(payload map[string]any) (normalizedPhone, error) {
	phoneObj := objectField(payload, "phone")
	rawNumber := firstNonEmpty(textField(payload, "e164_number"), textField(phoneObj, "e164_number"), textField(payload, "phone"), textField(payload, "phone_number"), textField(payload, "number"), textField(payload, "national_number"), textField(phoneObj, "national_number"))
	digits := nonDigits.ReplaceAllString(rawNumber, "")
	if digits == "" {
		return normalizedPhone{}, fmt.Errorf("phone is required")
	}
	callingCode := nonDigits.ReplaceAllString(firstNonEmpty(textField(payload, "country_calling_code"), textField(payload, "cc"), textField(phoneObj, "country_calling_code"), numericCountryCode(payload, phoneObj)), "")
	if callingCode == "" {
		return normalizedPhone{}, fmt.Errorf("country_calling_code is required")
	}
	parseInput := strings.TrimSpace(rawNumber)
	if !strings.HasPrefix(parseInput, "+") {
		if strings.HasPrefix(digits, callingCode) {
			parseInput = "+" + digits
		} else {
			parseInput = "+" + callingCode + digits
		}
	}
	parsed, err := phonenumbers.Parse(parseInput, "")
	if err != nil {
		return normalizedPhone{}, fmt.Errorf("phone parse failed: %w", err)
	}
	if fmt.Sprint(parsed.GetCountryCode()) != callingCode {
		return normalizedPhone{}, fmt.Errorf("phone country calling code does not match country_calling_code")
	}
	if !phonenumbers.IsPossibleNumber(parsed) {
		return normalizedPhone{}, errors.New(phoneNotPossibleMessage)
	}
	region := strings.ToUpper(firstNonEmpty(phonenumbers.GetRegionCodeForNumber(parsed), textField(payload, "country_iso2"), textField(phoneObj, "country_iso2")))
	return normalizedPhone{
		e164:           phonenumbers.Format(parsed, phonenumbers.E164),
		callingCode:    fmt.Sprint(parsed.GetCountryCode()),
		nationalNumber: phonenumbers.GetNationalSignificantNumber(parsed),
		countryISO2:    region,
	}, nil
}

func numericCountryCode(payload map[string]any, phoneObj map[string]any) string {
	value := firstNonEmpty(textField(payload, "country_code"), textField(phoneObj, "country_code"))
	if strings.TrimPrefix(value, "+") == nonDigits.ReplaceAllString(value, "") {
		return value
	}
	return ""
}

func textField(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	value, ok := data[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		return fmt.Sprintf("%.0f", typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func objectField(data map[string]any, key string) map[string]any {
	if data == nil {
		return map[string]any{}
	}
	if value, ok := data[key].(map[string]any); ok {
		return value
	}
	return map[string]any{}
}

func newRequestID(prefix string) string {
	var random [4]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s-%d-%s", prefix, time.Now().UnixNano(), hex.EncodeToString(random[:]))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeProtoJSON(w http.ResponseWriter, status int, value proto.Message) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	data, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(value)
	if err != nil {
		_, _ = w.Write([]byte("{}"))
		return
	}
	_, _ = w.Write(data)
}

func readProtoJSONPayload(w http.ResponseWriter, r *http.Request, maxBytes int64, value proto.Message, message string) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return true
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(body, value); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": message})
		return false
	}
	return true
}

func positiveInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func nonNegativeInt(value string, fallback int) int {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(trimmed)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func queryBool(value string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return fallback
	}
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func standaloneDashboard(dir string) http.Handler {
	files := spaFileServer(dir)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dashboard/wa" || strings.HasPrefix(r.URL.Path, "/dashboard/wa/") {
			http.Redirect(w, r, "/", http.StatusMovedPermanently)
			return
		}
		files.ServeHTTP(w, r)
	})
}

func spaFileServer(dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		path := filepath.Join(dir, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			http.ServeFile(w, r, path)
			return
		}
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
