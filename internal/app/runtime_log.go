package app

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

const defaultRuntimeLogCapacity = 300

type RuntimeLogEntry struct {
	ID            string            `json:"id"`
	At            time.Time         `json:"at"`
	Level         string            `json:"level"`
	Source        string            `json:"source"`
	Stage         string            `json:"stage"`
	Message       string            `json:"message"`
	CorrelationID string            `json:"correlation_id,omitempty"`
	Details       map[string]string `json:"details,omitempty"`
}

type RuntimeLogBuffer struct {
	mu      sync.Mutex
	nextID  uint64
	max     int
	entries []RuntimeLogEntry
}

func newRuntimeLogBuffer(max int) *RuntimeLogBuffer {
	if max <= 0 {
		max = defaultRuntimeLogCapacity
	}
	return &RuntimeLogBuffer{max: max}
}

func (s *Server) RecordRuntimeLog(entry RuntimeLogEntry) {
	if s == nil {
		return
	}
	if s.runtimeLogs == nil {
		s.runtimeLogs = newRuntimeLogBuffer(defaultRuntimeLogCapacity)
	}
	recorded := s.runtimeLogs.add(entry)
	details, _ := json.Marshal(recorded.Details)
	log.Printf(
		"wa_runtime_log id=%s level=%s source=%s stage=%s correlation=%s message=%s details=%s",
		recorded.ID,
		recorded.Level,
		recorded.Source,
		recorded.Stage,
		probeLogValue(recorded.CorrelationID),
		probeLogValue(recorded.Message),
		string(details),
	)
}

func (s *Server) RuntimeLogs(afterID string, limit int) []RuntimeLogEntry {
	if s == nil || s.runtimeLogs == nil {
		return nil
	}
	return s.runtimeLogs.list(afterID, limit)
}

func (b *RuntimeLogBuffer) add(entry RuntimeLogEntry) RuntimeLogEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	entry.ID = fmt.Sprintf("wlog_%012d", b.nextID)
	if entry.At.IsZero() {
		entry.At = time.Now().UTC()
	} else {
		entry.At = entry.At.UTC()
	}
	entry.Level = normalizeRuntimeLogLevel(entry.Level)
	entry.Source = sanitizeRuntimeLogText(entry.Source, 48)
	entry.Stage = sanitizeRuntimeLogText(entry.Stage, 64)
	entry.Message = sanitizeRuntimeLogText(entry.Message, 220)
	entry.CorrelationID = sanitizeRuntimeLogText(entry.CorrelationID, 96)
	entry.Details = sanitizeRuntimeLogDetails(entry.Details)
	b.entries = append(b.entries, entry)
	if overflow := len(b.entries) - b.max; overflow > 0 {
		b.entries = append([]RuntimeLogEntry(nil), b.entries[overflow:]...)
	}
	return entry
}

func (b *RuntimeLogBuffer) list(afterID string, limit int) []RuntimeLogEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	if limit <= 0 || limit > b.max {
		limit = 100
	}
	start := 0
	afterID = strings.TrimSpace(afterID)
	if afterID != "" {
		for idx, entry := range b.entries {
			if entry.ID == afterID {
				start = idx + 1
				break
			}
		}
	}
	if start >= len(b.entries) {
		return []RuntimeLogEntry{}
	}
	out := append([]RuntimeLogEntry(nil), b.entries[start:]...)
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

func normalizeRuntimeLogLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug", "info", "warn", "error":
		return strings.ToLower(strings.TrimSpace(level))
	default:
		return "info"
	}
}

func sanitizeRuntimeLogDetails(details map[string]string) map[string]string {
	if len(details) == 0 {
		return nil
	}
	out := make(map[string]string, len(details))
	for key, value := range details {
		key = sanitizeRuntimeLogText(key, 48)
		value = sanitizeRuntimeLogText(value, 180)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sanitizeRuntimeLogText(value string, maxRunes int) string {
	value = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "\n", " "), "\r", " "))
	if value == "" {
		return ""
	}
	value = urlCredentialPattern.ReplaceAllString(value, "${1}<redacted>@")
	value = sensitiveAssignmentPattern.ReplaceAllString(value, "${1}=<redacted>")
	runes := []rune(value)
	if maxRunes > 0 && len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "..."
	}
	return value
}

func runtimeLogValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []int:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, strconv.Itoa(item))
		}
		return strings.Join(parts, ",")
	case []string:
		return strings.Join(typed, ",")
	default:
		return fmt.Sprint(typed)
	}
}

func runtimeLogBool(value bool) string {
	return strconv.FormatBool(value)
}

func runtimeLogInt64(value int64) string {
	return strconv.FormatInt(value, 10)
}

func runtimeLogPhoneHash(phone *waappv1.PhoneTarget) string {
	if phone == nil || phone.GetE164Number() == "" {
		return ""
	}
	return stableID(phone.GetE164Number())
}

func runtimeLogPhoneTail(phoneValue string) string {
	digits := digitsOnly(phoneValue)
	if digits == "" {
		return ""
	}
	if len(digits) <= 4 {
		return "****"
	}
	return "****" + digits[len(digits)-4:]
}

func runtimeLogOrderDetails(order SMSReceiverOrder) map[string]string {
	return map[string]string{
		"provider":   order.Provider,
		"order_id":   redacted(order.ID),
		"country":    order.Country,
		"price":      order.Price,
		"service":    order.Service,
		"phone_tail": runtimeLogPhoneTail(order.Phone),
	}
}
