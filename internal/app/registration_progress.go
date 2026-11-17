package app

import (
	"context"
	"errors"
	"strings"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

type WAAccountRegistrationProgress struct {
	Success             bool           `json:"success"`
	WAAccountID         string         `json:"wa_account_id"`
	Stage               string         `json:"stage"`
	Label               string         `json:"label"`
	Description         string         `json:"description,omitempty"`
	VerificationRequest map[string]any `json:"verification_request,omitempty"`
	Registration        map[string]any `json:"registration,omitempty"`
}

func (s *Server) WAAccountRegistrationProgress(ctx context.Context, waAccountIDValue string) (WAAccountRegistrationProgress, error) {
	accountID, err := requireWAAccountID(waAccountIDValue)
	if err != nil {
		return WAAccountRegistrationProgress{}, err
	}
	account, err := s.getWAAccount(ctx, accountID)
	if err != nil {
		return WAAccountRegistrationProgress{}, err
	}
	verification, err := s.store.GetLatestVerificationRequestForAccount(ctx, accountID)
	if err != nil && !isRegistrationProgressNotFound(err) {
		return WAAccountRegistrationProgress{}, err
	}
	registration, err := s.store.GetLatestRegistrationForAccount(ctx, accountID)
	if err != nil && !isRegistrationProgressNotFound(err) {
		return WAAccountRegistrationProgress{}, err
	}
	stage, label, description := registrationProgressView(account, verification, registration)
	progress := WAAccountRegistrationProgress{
		Success:     true,
		WAAccountID: accountID,
		Stage:       stage,
		Label:       label,
		Description: description,
	}
	if verification != nil {
		if value := protoMap(verification); len(value) > 0 {
			progress.VerificationRequest = value
		}
	}
	if registration != nil {
		if value := protoMap(registration); len(value) > 0 {
			progress.Registration = value
		}
	}
	return progress, nil
}

func registrationProgressView(account *waappv1.WAAccount, verification *waappv1.VerificationCodeRequestRecord, registration *waappv1.RegistrationRecord) (string, string, string) {
	if account.GetStatus() == waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_ACTIVE || registration.GetStatus() == waappv1.RegistrationStatus_REGISTRATION_STATUS_REGISTERED {
		return "registered", "注册成功", ""
	}
	if registration != nil {
		switch registration.GetStatus() {
		case waappv1.RegistrationStatus_REGISTRATION_STATUS_SUBMITTED:
			return "otp_submitting", "已收到验证码，提交 WA 中", ""
		case waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED:
			if registrationProgressErrorLooksCancelled(registration.GetLastError()) {
				return "sms_order_cancelled", "接码订单已取消", registration.GetLastError().GetMessage()
			}
			if registrationProgressErrorLooksWARejected(registration.GetLastError()) {
				return "wa_rejected", "WA 拒绝注册", registration.GetLastError().GetMessage()
			}
			return "otp_consumed_failed", "验证码已消费但注册失败", registration.GetLastError().GetMessage()
		case waappv1.RegistrationStatus_REGISTRATION_STATUS_EXPIRED:
			return "sms_timeout", "验证码超时", registration.GetLastError().GetMessage()
		case waappv1.RegistrationStatus_REGISTRATION_STATUS_PENDING_CODE:
			return "waiting_sms", "等待平台短信", ""
		}
	}
	if verification != nil {
		switch verification.GetStatus() {
		case waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED:
			if registrationProgressErrorLooksCancelled(verification.GetLastError()) {
				return "sms_order_cancelled", "接码订单已取消", verification.GetLastError().GetMessage()
			}
			return "wa_rejected", "WA 拒绝注册", verification.GetLastError().GetMessage()
		case waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_EXPIRED:
			return "sms_timeout", "验证码超时", verification.GetLastError().GetMessage()
		case waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REQUESTED,
			waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_SENT,
			waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_WAITING:
			return "waiting_sms", "等待平台短信", ""
		}
	}
	if account.GetStatus() == waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_PENDING_REGISTRATION {
		return "waiting_sms", "等待平台短信", ""
	}
	return "unknown", "状态待确认", ""
}

func isRegistrationProgressNotFound(err error) bool {
	var appErr *AppError
	return errors.As(err, &appErr) && appErr.Code == waappv1.WaErrorCode_WA_ERROR_CODE_REGISTRATION_NOT_FOUND
}

func registrationProgressErrorLooksWARejected(err *waappv1.WaError) bool {
	if err == nil {
		return false
	}
	if err.GetCode() == waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED {
		return true
	}
	return registrationProgressMessageHasAny(err.GetMessage(), "blocked", "rejected", "reason=blocked", "拒绝")
}

func registrationProgressErrorLooksCancelled(err *waappv1.WaError) bool {
	if err == nil {
		return false
	}
	return registrationProgressMessageHasAny(err.GetMessage(), "cancel", "cancelled", "canceled", "取消")
}

func registrationProgressMessageHasAny(value string, needles ...string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return false
	}
	for _, needle := range needles {
		if strings.Contains(normalized, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}
