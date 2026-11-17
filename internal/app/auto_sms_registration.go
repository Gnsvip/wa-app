package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/nyaruka/phonenumbers"
)

const autoSMSRegistrationDelay = 1200 * time.Millisecond

type AutoSMSRegistrationInput struct {
	SMSReceiver    map[string]any
	TimeoutSeconds int
	TargetCount    int
}

type AutoSMSRegistrationTask struct {
	ID               string             `json:"id"`
	Running          bool               `json:"running"`
	Stopping         bool               `json:"stopping,omitempty"`
	Status           string             `json:"status"`
	Message          string             `json:"message"`
	Attempt          int                `json:"attempt"`
	TargetCount      int                `json:"target_count"`
	SuccessCount     int                `json:"success_count"`
	FailedCount      int                `json:"failed_count"`
	SkippedCount     int                `json:"skipped_count"`
	StartedAt        time.Time          `json:"started_at"`
	UpdatedAt        time.Time          `json:"updated_at"`
	StoppedAt        *time.Time         `json:"stopped_at,omitempty"`
	Countries        string             `json:"countries,omitempty"`
	MaxPrice         string             `json:"max_price,omitempty"`
	Provider         string             `json:"provider,omitempty"`
	Providers        []string           `json:"providers,omitempty"`
	Service          string             `json:"service,omitempty"`
	TimeoutSeconds   int                `json:"timeout_seconds,omitempty"`
	ProxyMode        string             `json:"proxy_mode,omitempty"`
	CurrentPhone     map[string]any     `json:"current_phone,omitempty"`
	SMSReceiverOrder *SMSReceiverOrder  `json:"sms_receiver_order,omitempty"`
	LastResult       map[string]any     `json:"last_result,omitempty"`
	ErrorMessage     string             `json:"error_message,omitempty"`
	cancel           context.CancelFunc `json:"-"`
	order            *SMSReceiverOrder  `json:"-"`
	stopRequested    bool               `json:"-"`
}

func (s *Server) StartAutoSMSRegistration(ctx context.Context, input AutoSMSRegistrationInput) (AutoSMSRegistrationTask, error) {
	if s == nil {
		return AutoSMSRegistrationTask{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "wa-app service is not configured", false)
	}
	if s.smsReceiver == nil || !s.smsReceiver.Configured() {
		return AutoSMSRegistrationTask{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "SMS receiver gateway is not configured", false)
	}
	return s.autoSMSRegistration.Start(ctx, input)
}

func (s *Server) AutoSMSRegistrationStatus() AutoSMSRegistrationTask {
	if s == nil || s.autoSMSRegistration == nil {
		return AutoSMSRegistrationTask{Status: "idle", Message: "未运行"}
	}
	return s.autoSMSRegistration.Status()
}

func (s *Server) StopAutoSMSRegistration(ctx context.Context) AutoSMSRegistrationTask {
	if s == nil || s.autoSMSRegistration == nil {
		return AutoSMSRegistrationTask{Status: "idle", Message: "未运行"}
	}
	return s.autoSMSRegistration.Stop(ctx)
}

type autoSMSRegistrationManager struct {
	mu      sync.Mutex
	server  *Server
	current *AutoSMSRegistrationTask
}

func newAutoSMSRegistrationManager(server *Server) *autoSMSRegistrationManager {
	return &autoSMSRegistrationManager{server: server}
}

func (m *autoSMSRegistrationManager) Start(ctx context.Context, input AutoSMSRegistrationInput) (AutoSMSRegistrationTask, error) {
	m.mu.Lock()
	if m.current != nil && m.current.Running {
		snapshot := cloneAutoSMSRegistrationTaskLocked(m.current)
		m.mu.Unlock()
		return snapshot, nil
	}
	request := smsReceiverAllocatePayload(map[string]any{"sms_receiver": input.SMSReceiver})
	if len(request) == 0 {
		m.mu.Unlock()
		return AutoSMSRegistrationTask{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "sms_receiver is required", false)
	}
	jobCtx, cancel := context.WithCancel(context.Background())
	now := time.Now().UTC()
	task := &AutoSMSRegistrationTask{
		ID:             m.server.ids.NewID("waauto_"),
		Running:        true,
		Status:         "running",
		Message:        "准备自动接码",
		StartedAt:      now,
		UpdatedAt:      now,
		Provider:       firstNonEmpty(textField(request, "provider"), "auto"),
		Providers:      autoSMSSelectedProviders(request),
		Service:        textField(request, "service"),
		Countries:      runtimeLogValue(request["countries"]),
		MaxPrice:       firstNonEmpty(runtimeLogValue(request["maxPrice"]), runtimeLogValue(request["max_price"])),
		TimeoutSeconds: autoSMSTimeoutSeconds(input.TimeoutSeconds),
		ProxyMode:      autoSMSProxyMode(request),
		TargetCount:    maxInt(0, input.TargetCount),
		cancel:         cancel,
	}
	m.current = task
	snapshot := cloneAutoSMSRegistrationTaskLocked(task)
	m.mu.Unlock()

	m.server.RecordRuntimeLog(RuntimeLogEntry{
		Level:   "info",
		Source:  "auto_sms_registration",
		Stage:   "start",
		Message: "自动接码任务已启动",
		Details: map[string]string{
			"task_id":   redacted(task.ID),
			"provider":  task.Provider,
			"countries": task.Countries,
			"max_price": task.MaxPrice,
			"target":    runtimeLogInt64(int64(task.TargetCount)),
		},
	})
	go m.run(jobCtx, task.ID, request, input.TimeoutSeconds, task.TargetCount)
	return snapshot, nil
}

func (m *autoSMSRegistrationManager) Status() AutoSMSRegistrationTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil {
		return AutoSMSRegistrationTask{Status: "idle", Message: "未运行"}
	}
	return cloneAutoSMSRegistrationTaskLocked(m.current)
}

func (m *autoSMSRegistrationManager) Stop(ctx context.Context) AutoSMSRegistrationTask {
	m.mu.Lock()
	if m.current == nil || !m.current.Running {
		snapshot := cloneAutoSMSRegistrationTaskLocked(m.current)
		m.mu.Unlock()
		if snapshot.Status == "" {
			return AutoSMSRegistrationTask{Status: "idle", Message: "未运行"}
		}
		return snapshot
	}
	m.current.stopRequested = true
	m.current.Stopping = true
	m.current.Message = "正在停止"
	m.current.UpdatedAt = time.Now().UTC()
	cancel := m.current.cancel
	order := cloneSMSReceiverOrderPtr(m.current.order)
	snapshot := cloneAutoSMSRegistrationTaskLocked(m.current)
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if order != nil && m.server.smsReceiver != nil && m.server.smsReceiver.Configured() {
		if err := m.server.smsReceiver.Cancel(ctx, *order); err != nil {
			m.server.RecordRuntimeLog(RuntimeLogEntry{
				Level:   "warn",
				Source:  "auto_sms_registration",
				Stage:   "stop",
				Message: "停止自动接码时取消接码订单失败",
				Details: map[string]string{"provider": order.Provider, "order_id": redacted(order.ID), "error": safeInternalErrorMessage(err)},
			})
		}
	}
	m.server.RecordRuntimeLog(RuntimeLogEntry{
		Level:   "info",
		Source:  "auto_sms_registration",
		Stage:   "stop",
		Message: "已请求停止自动接码任务",
		Details: map[string]string{"task_id": redacted(snapshot.ID)},
	})
	return snapshot
}

func (m *autoSMSRegistrationManager) run(ctx context.Context, taskID string, request map[string]any, timeoutSeconds int, targetCount int) {
	for attempt := 1; ; attempt++ {
		if ctx.Err() != nil || m.stopRequested(taskID) {
			m.finishStopped(taskID)
			return
		}
		m.update(taskID, func(task *AutoSMSRegistrationTask) {
			task.Attempt = attempt
			task.Message = "第 " + runtimeLogInt64(int64(attempt)) + " 次拿号"
			task.Status = "running"
			task.SMSReceiverOrder = nil
			task.order = nil
			task.CurrentPhone = nil
			task.LastResult = nil
		})
		attemptRequest := autoSMSAttemptRequest(request, attempt)
		m.update(taskID, func(task *AutoSMSRegistrationTask) {
			task.Provider = firstNonEmpty(textField(attemptRequest, "provider"), "auto")
			task.Service = textField(attemptRequest, "service")
			task.Countries = runtimeLogValue(attemptRequest["countries"])
		})
		order, err := m.server.smsReceiver.Allocate(ctx, attemptRequest)
		if err != nil {
			m.recordAllocateAttemptError(taskID, attempt, attemptRequest, err)
			if m.handleAttemptError(ctx, taskID, attempt, nil, err) {
				return
			}
			continue
		}
		m.update(taskID, func(task *AutoSMSRegistrationTask) {
			task.order = &order
			task.SMSReceiverOrder = cloneSMSReceiverOrderPtr(&order)
			task.CurrentPhone = autoSMSPhoneSummary(order.Phone)
		})
		phone, err := autoSMSPhoneTarget(order.Phone)
		if err != nil {
			if m.handleAttemptError(ctx, taskID, attempt, &order, err) {
				return
			}
			continue
		}
		m.update(taskID, func(task *AutoSMSRegistrationTask) {
			task.Message = "第 " + runtimeLogInt64(int64(attempt)) + " 次检测 WA"
			task.CurrentPhone = autoSMSPhoneTargetSummary(phone)
		})
		probePayload := autoSMSPhonePayload(phone)
		probePayload["request_id"] = m.server.ids.NewID("wa-req_")
		probePayload["job_id"] = taskID
		probeResult, err := m.server.ProbeNumberSMS(ctx, probePayload)
		if err != nil {
			if m.handleAttemptError(ctx, taskID, attempt, &order, err) {
				return
			}
			continue
		}
		m.update(taskID, func(task *AutoSMSRegistrationTask) {
			task.LastResult = cloneMap(probeResult)
		})
		if autoSMSProbeRejected(probeResult) {
			_ = m.cancelOrder(context.Background(), taskID, &order)
			m.clearOrder(taskID)
			m.update(taskID, func(task *AutoSMSRegistrationTask) {
				task.SkippedCount++
				task.Message = "第 " + runtimeLogInt64(int64(attempt)) + " 次未通过，继续"
				task.LastResult = cloneMap(probeResult)
			})
			if !m.waitNext(ctx, taskID) {
				return
			}
			continue
		}
		m.update(taskID, func(task *AutoSMSRegistrationTask) {
			task.Message = "第 " + runtimeLogInt64(int64(attempt)) + " 次请求验证码"
		})
		registerPayload := autoSMSPhonePayload(phone)
		registerPayload["request_id"] = m.server.ids.NewID("wa-req_")
		registerPayload["job_id"] = taskID
		registerPayload["delivery_method"] = registrationMethodName(waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS, "sms")
		registerPayload["sms_receiver_order"] = smsReceiverOrderMap(order)
		registration, err := m.server.StartRegistration(ctx, registerPayload)
		if err != nil {
			if m.handleAttemptError(ctx, taskID, attempt, &order, err) {
				return
			}
			continue
		}
		if autoSMSWorkflowRejected(registration) {
			_ = m.cancelOrder(context.Background(), taskID, &order)
			m.clearOrder(taskID)
			m.update(taskID, func(task *AutoSMSRegistrationTask) {
				task.FailedCount++
				task.Message = "第 " + runtimeLogInt64(int64(attempt)) + " 次 WA 拒绝，继续"
				task.LastResult = cloneMap(registration)
			})
			if !m.waitNext(ctx, taskID) {
				return
			}
			continue
		}
		waitingOrder := order
		if responseOrder := smsReceiverOrderFromMap(objectField(registration, "sms_receiver_order")); responseOrder.Provider != "" || responseOrder.ID != "" || responseOrder.Phone != "" {
			waitingOrder = responseOrder
		}
		waAccountID := textField(registration, "wa_account_id")
		verificationRequestID := textField(registration, "verification_request_id")
		if waAccountID == "" {
			_ = m.cancelOrder(context.Background(), taskID, &waitingOrder)
			m.clearOrder(taskID)
			m.update(taskID, func(task *AutoSMSRegistrationTask) {
				task.FailedCount++
				task.Message = "第 " + runtimeLogInt64(int64(attempt)) + " 次未返回账号，继续"
				task.LastResult = cloneMap(registration)
			})
			if !m.waitNext(ctx, taskID) {
				return
			}
			continue
		}
		m.update(taskID, func(task *AutoSMSRegistrationTask) {
			task.order = &waitingOrder
			task.SMSReceiverOrder = cloneSMSReceiverOrderPtr(&waitingOrder)
			task.Message = "第 " + runtimeLogInt64(int64(attempt)) + " 次等待短信"
			task.LastResult = cloneMap(registration)
		})
		otpPayload := map[string]any{
			"wa_account_id":           waAccountID,
			"verification_request_id": verificationRequestID,
			"timeout_seconds":         autoSMSTimeoutSeconds(timeoutSeconds),
			"request_id":              m.server.ids.NewID("wa-req_"),
			"job_id":                  taskID,
		}
		otpResult, err := (&actionGateway{server: m.server}).receiveSMSOTP(ctx, otpPayload)
		if err != nil {
			if m.handleAttemptError(ctx, taskID, attempt, &waitingOrder, err) {
				return
			}
			continue
		}
		if autoSMSWorkflowRejected(otpResult) {
			_ = m.cancelOrder(context.Background(), taskID, &waitingOrder)
			m.clearOrder(taskID)
			m.update(taskID, func(task *AutoSMSRegistrationTask) {
				task.FailedCount++
				task.Message = "第 " + runtimeLogInt64(int64(attempt)) + " 次 OTP 失败，继续"
				task.LastResult = cloneMap(otpResult)
			})
			if !m.waitNext(ctx, taskID) {
				return
			}
			continue
		}
		reachedTarget := m.recordSuccess(taskID, attempt, targetCount, otpResult)
		m.server.RecordRuntimeLog(RuntimeLogEntry{
			Level:   "info",
			Source:  "auto_sms_registration",
			Stage:   "success",
			Message: "自动接码注册成功",
			Details: map[string]string{"task_id": redacted(taskID), "success_count": runtimeLogInt64(int64(m.successCount(taskID))), "target": runtimeLogInt64(int64(targetCount))},
		})
		if reachedTarget {
			break
		}
		m.clearOrder(taskID)
		if !m.waitNext(ctx, taskID) {
			return
		}
	}
}

func (m *autoSMSRegistrationManager) handleAttemptError(ctx context.Context, taskID string, attempt int, order *SMSReceiverOrder, err error) bool {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || m.stopRequested(taskID) {
		if order != nil {
			_ = m.cancelOrder(context.Background(), taskID, order)
		}
		m.finishStopped(taskID)
		return true
	}
	if order != nil {
		if cancelErr := m.cancelOrder(context.Background(), taskID, order); cancelErr != nil {
			m.server.RecordRuntimeLog(RuntimeLogEntry{
				Level:   "warn",
				Source:  "auto_sms_registration",
				Stage:   "cancel",
				Message: "接码订单取消失败，已忽略并继续自动接码",
				Details: map[string]string{"task_id": redacted(taskID), "error": safeInternalErrorMessage(cancelErr)},
			})
		}
		m.clearOrder(taskID)
	}
	message := safeInternalErrorMessage(err)
	m.update(taskID, func(task *AutoSMSRegistrationTask) {
		task.FailedCount++
		task.Message = "第 " + runtimeLogInt64(int64(attempt)) + " 次失败：" + shortRuntimeMessage(message) + "，继续"
		task.ErrorMessage = message
	})
	return !m.waitNext(ctx, taskID)
}

func (m *autoSMSRegistrationManager) recordSuccess(taskID string, attempt int, targetCount int, result map[string]any) bool {
	reachedTarget := false
	m.update(taskID, func(task *AutoSMSRegistrationTask) {
		task.SuccessCount++
		task.Status = "running"
		task.LastResult = cloneMap(result)
		task.order = nil
		task.SMSReceiverOrder = nil
		if targetCount > 0 && task.SuccessCount >= targetCount {
			task.Status = "success"
			task.Message = "已完成目标 " + runtimeLogInt64(int64(task.SuccessCount)) + "/" + runtimeLogInt64(int64(targetCount))
			task.Running = false
			task.Stopping = false
			stopped := time.Now().UTC()
			task.StoppedAt = &stopped
			reachedTarget = true
		} else {
			task.Message = "第 " + runtimeLogInt64(int64(attempt)) + " 次注册成功，继续"
		}
	})
	return reachedTarget
}

func (m *autoSMSRegistrationManager) successCount(taskID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil || m.current.ID != taskID {
		return 0
	}
	return m.current.SuccessCount
}

func (m *autoSMSRegistrationManager) recordAllocateAttemptError(taskID string, attempt int, request map[string]any, err error) {
	provider := firstNonEmpty(textField(request, "provider"), "auto")
	message := "自动接码拿号失败，继续"
	errMessage := safeInternalErrorMessage(err)
	normalized := strings.ToLower(errMessage)
	if autoSMSPriceLimitError(normalized) {
		message = "自动接码价格超限，已取消订单并继续"
	} else if strings.Contains(normalized, "no mapped countries") {
		message = "自动接码已跳过无国家映射的平台"
	}
	m.server.RecordRuntimeLog(RuntimeLogEntry{
		Level:   "warn",
		Source:  "auto_sms_registration",
		Stage:   "allocate",
		Message: message,
		Details: map[string]string{
			"task_id":  redacted(taskID),
			"attempt":  runtimeLogInt64(int64(attempt)),
			"provider": provider,
			"error":    errMessage,
		},
	})
}

func (m *autoSMSRegistrationManager) waitNext(ctx context.Context, taskID string) bool {
	timer := time.NewTimer(autoSMSRegistrationDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		m.finishStopped(taskID)
		return false
	case <-timer.C:
		if m.stopRequested(taskID) {
			m.finishStopped(taskID)
			return false
		}
		return true
	}
}

func (m *autoSMSRegistrationManager) cancelOrder(ctx context.Context, taskID string, order *SMSReceiverOrder) error {
	if order == nil || m.server.smsReceiver == nil || !m.server.smsReceiver.Configured() {
		return nil
	}
	err := m.server.smsReceiver.Cancel(ctx, *order)
	if err != nil {
		m.server.RecordRuntimeLog(RuntimeLogEntry{
			Level:   "warn",
			Source:  "auto_sms_registration",
			Stage:   "cancel",
			Message: "自动接码取消接码订单失败",
			Details: map[string]string{"task_id": redacted(taskID), "provider": order.Provider, "order_id": redacted(order.ID), "error": safeInternalErrorMessage(err)},
		})
		return err
	}
	m.server.RecordRuntimeLog(RuntimeLogEntry{
		Level:   "info",
		Source:  "auto_sms_registration",
		Stage:   "cancel",
		Message: "自动接码已取消接码订单",
		Details: map[string]string{"task_id": redacted(taskID), "provider": order.Provider, "order_id": redacted(order.ID)},
	})
	return nil
}

func (m *autoSMSRegistrationManager) clearOrder(taskID string) {
	m.update(taskID, func(task *AutoSMSRegistrationTask) {
		task.order = nil
		task.SMSReceiverOrder = nil
	})
}

func (m *autoSMSRegistrationManager) finishStopped(taskID string) {
	m.update(taskID, func(task *AutoSMSRegistrationTask) {
		task.Running = false
		task.Stopping = false
		task.stopRequested = false
		task.Status = "stopped"
		task.Message = "自动接码已停止"
		task.order = nil
		task.SMSReceiverOrder = nil
		stopped := time.Now().UTC()
		task.StoppedAt = &stopped
	})
}

func (m *autoSMSRegistrationManager) stopRequested(taskID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current != nil && m.current.ID == taskID && m.current.stopRequested
}

func (m *autoSMSRegistrationManager) update(taskID string, mutate func(*AutoSMSRegistrationTask)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil || m.current.ID != taskID {
		return
	}
	mutate(m.current)
	m.current.UpdatedAt = time.Now().UTC()
}

func cloneAutoSMSRegistrationTaskLocked(task *AutoSMSRegistrationTask) AutoSMSRegistrationTask {
	if task == nil {
		return AutoSMSRegistrationTask{Status: "idle", Message: "未运行"}
	}
	snapshot := *task
	snapshot.cancel = nil
	snapshot.order = nil
	snapshot.stopRequested = false
	snapshot.SMSReceiverOrder = cloneSMSReceiverOrderPtr(task.SMSReceiverOrder)
	snapshot.LastResult = cloneMap(task.LastResult)
	snapshot.CurrentPhone = cloneMap(task.CurrentPhone)
	snapshot.Providers = append([]string(nil), task.Providers...)
	if task.StoppedAt != nil {
		stopped := *task.StoppedAt
		snapshot.StoppedAt = &stopped
	}
	return snapshot
}

func cloneSMSReceiverOrderPtr(order *SMSReceiverOrder) *SMSReceiverOrder {
	if order == nil {
		return nil
	}
	cloned := *order
	return &cloned
}

func autoSMSAttemptRequest(base map[string]any, attempt int) map[string]any {
	request := cloneMap(base)
	if strings.TrimSpace(textField(request, "provider")) != "" {
		return request
	}
	order := autoSMSProviderOrder(request)
	provider := order[(attempt-1)%len(order)]
	request["provider"] = provider
	return request
}

func autoSMSProviderOrder(request map[string]any) []string {
	order := normalizeSMSProviderList(request["providers"])
	if len(order) == 0 {
		order = normalizeSMSProviderList(request["providerOrder"])
	}
	if len(order) == 0 {
		order = normalizeSMSProviderList(request["provider_order"])
	}
	if len(order) == 0 {
		return []string{"smsbower", "herosms", "5sim"}
	}
	return order
}

func autoSMSSelectedProviders(request map[string]any) []string {
	if provider := strings.ToLower(strings.TrimSpace(textField(request, "provider"))); provider != "" {
		return normalizeSMSProviderList([]string{provider})
	}
	return autoSMSProviderOrder(request)
}

func autoSMSProxyMode(request map[string]any) string {
	if value, ok := request["proxy"]; ok {
		if strings.TrimSpace(fmt.Sprint(value)) == "" {
			return "direct"
		}
		return "config"
	}
	return "config"
}

func normalizeSMSProviderList(value any) []string {
	var raw []string
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			raw = append(raw, fmt.Sprint(item))
		}
	case []string:
		raw = append(raw, typed...)
	case string:
		raw = strings.FieldsFunc(typed, func(r rune) bool {
			return r == ',' || r == '，' || r == '、' || r == ' ' || r == '\t' || r == '\n'
		})
	}
	allowed := map[string]bool{"smsbower": true, "herosms": true, "5sim": true}
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		provider := strings.ToLower(strings.TrimSpace(item))
		if !allowed[provider] || seen[provider] {
			continue
		}
		seen[provider] = true
		out = append(out, provider)
	}
	return out
}

func autoSMSPhoneTarget(raw string) (*waappv1.PhoneTarget, error) {
	digits := digitsOnly(raw)
	if digits == "" {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "phone is required", false)
	}
	parseInput := strings.TrimSpace(raw)
	if !strings.HasPrefix(parseInput, "+") {
		parseInput = "+" + digits
	}
	parsed, err := phonenumbers.Parse(parseInput, "")
	if err != nil {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "phone parse failed: "+err.Error(), false)
	}
	if !phonenumbers.IsPossibleNumber(parsed) {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "手机号位数不符合国家规则，请检查接码平台返回号码。", false)
	}
	return &waappv1.PhoneTarget{
		E164Number:         phonenumbers.Format(parsed, phonenumbers.E164),
		CountryCallingCode: strconv.FormatInt(int64(parsed.GetCountryCode()), 10),
		NationalNumber:     phonenumbers.GetNationalSignificantNumber(parsed),
		CountryIso2:        strings.ToUpper(phonenumbers.GetRegionCodeForNumber(parsed)),
	}, nil
}

func autoSMSPhonePayload(phone *waappv1.PhoneTarget) map[string]any {
	normalized := normalizePhone(phone)
	return map[string]any{
		"country_region":       normalized.GetCountryIso2(),
		"country_code":         normalized.GetCountryCallingCode(),
		"country_calling_code": normalized.GetCountryCallingCode(),
		"country_iso2":         normalized.GetCountryIso2(),
		"region":               normalized.GetCountryIso2(),
		"phone": map[string]any{
			"e164_number":          normalized.GetE164Number(),
			"country_calling_code": normalized.GetCountryCallingCode(),
			"national_number":      normalized.GetNationalNumber(),
			"country_iso2":         normalized.GetCountryIso2(),
		},
	}
}

func autoSMSPhoneTargetSummary(phone *waappv1.PhoneTarget) map[string]any {
	if phone == nil {
		return nil
	}
	return map[string]any{
		"e164_number":          phone.GetE164Number(),
		"country_calling_code": phoneCC(phone),
		"country_iso2":         strings.ToUpper(phone.GetCountryIso2()),
		"national_number":      phone.GetNationalNumber(),
		"phone_tail":           runtimeLogPhoneTail(phone.GetE164Number()),
	}
}

func autoSMSPhoneSummary(phone string) map[string]any {
	if target, err := autoSMSPhoneTarget(phone); err == nil {
		return autoSMSPhoneTargetSummary(target)
	}
	return map[string]any{"phone_tail": runtimeLogPhoneTail(phone)}
}

func autoSMSProbeRejected(result map[string]any) bool {
	if result == nil {
		return true
	}
	status := objectField(result, "phone_status")
	if boolField(result, "request_failed") || boolField(status, "request_failed") || boolField(status, "blocked") {
		return true
	}
	if smsAvailable, known := optionalBoolField(status, "sms_available"); known && !smsAvailable {
		return true
	}
	if canRegister, known := optionalBoolField(status, "can_register"); known && !canRegister {
		return true
	}
	return false
}

func autoSMSWorkflowRejected(result map[string]any) bool {
	if result == nil {
		return true
	}
	if success, known := optionalBoolField(result, "success"); known && !success {
		return true
	}
	return textField(result, "error_message") != "" || boolField(result, "request_failed") || boolField(objectField(result, "phone_status"), "blocked")
}

func autoSMSTimeoutSeconds(value int) int {
	if value > 0 {
		return value
	}
	return 180
}

func autoSMSPriceLimitError(normalized string) bool {
	return strings.Contains(normalized, "wrong_max_price") || strings.Contains(normalized, "exceeds maxprice")
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func shortRuntimeMessage(message string) string {
	normalized := strings.TrimSpace(strings.Join(strings.Fields(message), " "))
	runes := []rune(normalized)
	if len(runes) > 34 {
		return string(runes[:34]) + "..."
	}
	if normalized == "" {
		return "未知原因"
	}
	return normalized
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
