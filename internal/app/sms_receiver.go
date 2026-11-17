package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

const defaultSMSReceiverTimeout = 15 * time.Second

type SMSReceiverClient struct {
	baseURL    string
	apiToken   string
	httpClient *http.Client
}

type SMSReceiverOrder struct {
	Provider  string `json:"provider"`
	ID        string `json:"id"`
	Phone     string `json:"phone"`
	Country   string `json:"country,omitempty"`
	Price     string `json:"price,omitempty"`
	Service   string `json:"service,omitempty"`
	ProxyMode string `json:"proxy_mode,omitempty"`
	SMSURL    string `json:"sms_url,omitempty"`
	SMSLine   string `json:"sms_line,omitempty"`
}

type SMSReceiverCountry struct {
	Provider  string         `json:"provider"`
	ID        int            `json:"id"`
	Value     string         `json:"value,omitempty"`
	Name      string         `json:"name"`
	Count     int            `json:"count,omitempty"`
	Cost      float64        `json:"cost,omitempty"`
	Providers map[string]any `json:"providers,omitempty"`
}

func NewSMSReceiverClient(baseURL string, apiToken string) (*SMSReceiverClient, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, nil
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "WA_APP_SMS_GATEWAY_URL is invalid", false)
	}
	return &SMSReceiverClient{
		baseURL:    baseURL,
		apiToken:   strings.TrimSpace(apiToken),
		httpClient: &http.Client{Timeout: defaultSMSReceiverTimeout},
	}, nil
}

func (c *SMSReceiverClient) Configured() bool {
	return c != nil && c.baseURL != ""
}

func (c *SMSReceiverClient) Allocate(ctx context.Context, payload map[string]any) (SMSReceiverOrder, error) {
	data, err := c.post(ctx, "allocate", payload)
	if err != nil {
		return SMSReceiverOrder{}, err
	}
	orderData := objectField(data, "order")
	order := SMSReceiverOrder{
		Provider: textField(orderData, "provider"),
		ID:       textField(orderData, "id"),
		Phone:    textField(orderData, "phone"),
		Country:  textField(orderData, "country"),
		Price:    textField(orderData, "price"),
		Service:  textField(orderData, "service"),
		SMSURL:   firstNonEmpty(textField(orderData, "sms_url"), textField(data, "sms_url")),
		SMSLine:  firstNonEmpty(textField(orderData, "sms_line"), textField(data, "sms_line")),
	}
	if _, explicitProxy := payload["proxy"].(string); explicitProxy && textField(payload, "proxy") == "" {
		order.ProxyMode = "direct"
	}
	if order.Provider == "" || order.ID == "" || order.Phone == "" {
		return SMSReceiverOrder{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "SMS receiver returned an incomplete order", true)
	}
	return order, nil
}

func (c *SMSReceiverClient) Countries(ctx context.Context, payload map[string]any) ([]SMSReceiverCountry, map[string]string, error) {
	data, err := c.post(ctx, "countries", payload)
	if err != nil {
		return nil, nil, err
	}
	items, _ := data["countries"].([]any)
	countries := make([]SMSReceiverCountry, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := int(numberField(row, "id"))
		value := textField(row, "value")
		if id <= 0 && value == "" {
			continue
		}
		countries = append(countries, SMSReceiverCountry{
			Provider:  textField(row, "provider"),
			ID:        id,
			Value:     value,
			Name:      textField(row, "name"),
			Count:     int(numberField(row, "count")),
			Cost:      floatField(row, "cost"),
			Providers: objectField(row, "providers"),
		})
	}
	errors := map[string]string{}
	if errorRows, ok := data["errors"].(map[string]any); ok {
		for provider, value := range errorRows {
			message := strings.TrimSpace(fmt.Sprint(value))
			if message != "" {
				errors[provider] = message
			}
		}
	}
	return countries, errors, nil
}

func (c *SMSReceiverClient) Settings(ctx context.Context) (map[string]any, error) {
	return c.post(ctx, "settings", map[string]any{})
}

func (c *SMSReceiverClient) SaveSettings(ctx context.Context, payload map[string]any) (map[string]any, error) {
	return c.post(ctx, "save-settings", payload)
}

func (c *SMSReceiverClient) TestProxy(ctx context.Context, payload map[string]any) (map[string]any, error) {
	return c.postWithTimeout(ctx, "test-proxy", payload, 20*time.Second)
}

func (c *SMSReceiverClient) Balance(ctx context.Context, payload map[string]any) (map[string]any, error) {
	return c.postWithTimeout(ctx, "balance", payload, 20*time.Second)
}

func (c *SMSReceiverClient) WaitCode(ctx context.Context, order SMSReceiverOrder, timeout time.Duration, interval time.Duration, markReady bool) (string, error) {
	return c.WaitCodeWithOptions(ctx, order, timeout, interval, markReady, true)
}

func (c *SMSReceiverClient) WaitCodeWithOptions(ctx context.Context, order SMSReceiverOrder, timeout time.Duration, interval time.Duration, markReady bool, cancelOnFailure bool) (string, error) {
	payload := orderActionPayload(order)
	payload["markReady"] = markReady
	payload["cancelOnFailure"] = cancelOnFailure
	if timeout > 0 {
		payload["timeout"] = int(timeout / time.Second)
	}
	if interval > 0 {
		payload["interval"] = int(interval / time.Second)
	}
	data, err := c.postWithTimeout(ctx, "wait-code", payload, timeout+30*time.Second)
	if err != nil {
		return "", err
	}
	code := textField(data, "code")
	if code == "" {
		return "", NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "SMS receiver did not return a code", true)
	}
	return code, nil
}

func (c *SMSReceiverClient) Ready(ctx context.Context, order SMSReceiverOrder) error {
	_, err := c.post(ctx, "ready", orderActionPayload(order))
	return err
}

func (c *SMSReceiverClient) Code(ctx context.Context, order SMSReceiverOrder) (string, bool, error) {
	data, err := c.post(ctx, "code", orderActionPayload(order))
	if err != nil {
		return "", false, err
	}
	code := textField(data, "code")
	received := code != ""
	if value, known := optionalBoolField(data, "received"); known {
		received = value
	}
	return code, received, nil
}

func (c *SMSReceiverClient) Finish(ctx context.Context, order SMSReceiverOrder) error {
	_, err := c.post(ctx, "finish", orderActionPayload(order))
	return err
}

func (c *SMSReceiverClient) Cancel(ctx context.Context, order SMSReceiverOrder) error {
	_, err := c.post(ctx, "cancel", orderActionPayload(order))
	return err
}

func (c *SMSReceiverClient) post(ctx context.Context, action string, payload map[string]any) (map[string]any, error) {
	return c.postWithTimeout(ctx, action, payload, defaultSMSReceiverTimeout)
}

func (c *SMSReceiverClient) postWithTimeout(ctx context.Context, action string, payload map[string]any, timeout time.Duration) (map[string]any, error) {
	if !c.Configured() {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "SMS receiver gateway is not configured", false)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+strings.Trim(action, "/"), bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiToken)
	}
	client := c.httpClient
	if timeout > 0 && timeout != defaultSMSReceiverTimeout {
		client = &http.Client{Timeout: timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "SMS receiver request failed: "+err.Error(), true)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	data := map[string]any{}
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &data); err != nil {
			return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, fmt.Sprintf("SMS receiver returned invalid JSON with HTTP %d", resp.StatusCode), true)
		}
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		message := firstNonEmpty(textField(data, "error"), textField(data, "message"))
		if message != "" {
			return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, fmt.Sprintf("SMS receiver returned HTTP %d: %s", resp.StatusCode, message), true)
		}
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, fmt.Sprintf("SMS receiver returned HTTP %d", resp.StatusCode), true)
	}
	if ok, known := optionalBoolField(data, "ok"); known && !ok {
		message := firstNonEmpty(textField(data, "error"), "SMS receiver request failed")
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, message, true)
	}
	return data, nil
}

func orderActionPayload(order SMSReceiverOrder) map[string]any {
	payload := map[string]any{
		"provider": order.Provider,
		"id":       order.ID,
	}
	if order.ProxyMode == "direct" {
		payload["proxy"] = ""
	}
	return payload
}

func smsReceiverOrderMap(order SMSReceiverOrder) map[string]any {
	payload := map[string]any{
		"provider": order.Provider,
		"id":       order.ID,
		"phone":    order.Phone,
		"country":  order.Country,
		"price":    order.Price,
		"service":  order.Service,
	}
	if order.ProxyMode != "" {
		payload["proxy_mode"] = order.ProxyMode
	}
	if order.SMSURL != "" {
		payload["sms_url"] = order.SMSURL
	}
	if order.SMSLine != "" {
		payload["sms_line"] = order.SMSLine
	}
	return payload
}

func smsReceiverOrderFromMap(data map[string]any) SMSReceiverOrder {
	proxyMode := firstNonEmpty(textField(data, "proxy_mode"), textField(data, "proxyMode"))
	if proxyMode == "" {
		if _, explicitProxy := data["proxy"].(string); explicitProxy && textField(data, "proxy") == "" {
			proxyMode = "direct"
		}
	}
	return SMSReceiverOrder{
		Provider:  textField(data, "provider"),
		ID:        textField(data, "id"),
		Phone:     textField(data, "phone"),
		Country:   textField(data, "country"),
		Price:     textField(data, "price"),
		Service:   textField(data, "service"),
		ProxyMode: proxyMode,
		SMSURL:    textField(data, "sms_url"),
		SMSLine:   textField(data, "sms_line"),
	}
}
