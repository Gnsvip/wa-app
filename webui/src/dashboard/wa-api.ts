import type { GetTwoFactorAuthStatusResponse, RemoveAccountProfilePictureResponse, RequestAccountEmailOtpResponse, SetAccountEmailResponse, SetAccountProfileNameResponse, SetAccountProfilePictureResponse, SetTwoFactorAuthSettingsResponse, VerifyAccountEmailOtpResponse } from '../proto/byte/v/forge/waapp/v1/account_settings';
import type { DeleteWAContactResponse, ListWAContactsResponse, ResolveWAContactsResponse, WAContact } from '../proto/byte/v/forge/waapp/v1/contacts';
import type { ListAccountOtpMessagesResponse } from '../proto/byte/v/forge/waapp/v1/extraction';
import type { DeleteAccountMessagesResponse, GetLongConnectionStatusResponse, ListAccountMessagesResponse, LongConnectionState, MarkAccountMessagesReadResponse, SendTextMessageResponse } from '../proto/byte/v/forge/waapp/v1/messaging';
import type { DeletePendingRegistrationWAAccountsResponse, DeleteWAAccountResponse, ListClientProfilesResponse, ListWAAccountsResponse, WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import type { RegistrationRecord, VerificationCodeRequestRecord, VerificationDeliveryMethod } from '../proto/byte/v/forge/waapp/v1/registration';

export const ACCOUNT_PAGE_SIZE = 100;

export type WaPhoneInput = { region: string; phone: string; e164_number: string; country_calling_code: string; country_iso2: string };
export type SmsReceiverOrder = { provider: string; id: string; phone: string; country?: string; price?: string; service?: string; proxy_mode?: string; sms_url?: string; sms_line?: string };
export type SmsReceiverAllocateInput = { provider?: string; providers?: string[]; service?: string; countries?: Array<number | string>; minPrice?: number; maxPrice?: number; timeoutSeconds?: number; intervalSeconds?: number; pollIntervalSeconds?: number; linkTtlSeconds?: number; proxy?: string; wa_account_id?: string; phone?: string };
export type SmsReceiverCountry = { provider?: string; id?: number; value?: string; name?: string; count?: number; cost?: number; providers?: Record<string, number | string> };
export type SmsReceiverProviderSetting = { enabled?: boolean; apiKeyConfigured?: boolean; apiKeyMasked?: string };
export type SmsReceiverSettings = { proxy?: string; providerOrder?: string[]; service?: string; countries?: Array<number | string>; maxPrice?: number; timeoutSeconds?: number; providers?: Record<string, SmsReceiverProviderSetting>; fiveSim?: Record<string, unknown> };
export type SmsReceiverSettingsUpdate = { proxy?: string; providerOrder?: string[]; service?: string; maxPrice?: number; timeoutSeconds?: number; providers?: Record<string, { apiKey?: string; clearApiKey?: boolean }> };
export type SmsReceiverProxyTestResult = { ok?: boolean; mode?: 'direct' | 'proxy'; proxyConfigured?: boolean; target?: string; elapsedMs?: number; ip?: string; error?: string };
export type SmsReceiverProxyTestResponse = { success?: boolean; result?: SmsReceiverProxyTestResult; error_message?: string };
export type SmsReceiverBalanceResult = { ok?: boolean; provider?: string; balance?: string; currency?: string; error?: string };
export type SmsReceiverBalanceResponse = { success?: boolean; balances?: Record<string, SmsReceiverBalanceResult>; error_message?: string };
export type AutoSmsRegistrationTask = { id?: string; running?: boolean; stopping?: boolean; status?: string; message?: string; attempt?: number; target_count?: number; success_count?: number; failed_count?: number; skipped_count?: number; started_at?: string; updated_at?: string; stopped_at?: string; countries?: string; max_price?: string; provider?: string; providers?: string[]; service?: string; timeout_seconds?: number; proxy_mode?: string; current_phone?: Record<string, unknown>; sms_receiver_order?: SmsReceiverOrder; last_result?: WaWorkflowResponse; error_message?: string };
export type AutoSmsRegistrationResponse = { success?: boolean; task?: AutoSmsRegistrationTask; error_message?: string };
export type WaRuntimeLogEntry = { id: string; at: string; level: 'debug' | 'info' | 'warn' | 'error'; source: string; stage: string; message: string; correlation_id?: string; details?: Record<string, string> };
export type WaRuntimeLogResponse = { logs?: WaRuntimeLogEntry[]; now?: string };
export type WaAccountRegistrationProgressStage = 'waiting_sms' | 'otp_submitting' | 'registered' | 'wa_rejected' | 'sms_timeout' | 'sms_order_cancelled' | 'otp_consumed_failed' | 'unknown';
export type WaAccountRegistrationProgress = { success?: boolean; wa_account_id?: string; stage?: WaAccountRegistrationProgressStage; label?: string; description?: string; verification_request?: VerificationCodeRequestRecord; registration?: RegistrationRecord; error_message?: string };
export type WaWorkflowResponse = { success?: boolean; passed?: boolean; request_failed?: boolean; retry_after_seconds?: number; status?: string; error_message?: string; reject_reason?: string; wa_account_id?: string; client_profile_id?: string; protocol_profile_id?: string; verification_request_id?: string; delivery_method?: string; method?: string; registration_phase?: string; method_statuses?: unknown[]; phone_status?: Record<string, unknown>; account_probe?: Record<string, unknown>; sms_probe?: Record<string, unknown>; phone?: Record<string, unknown>; proxy?: Record<string, unknown>; verification_request?: Record<string, unknown>; account_transfer_challenge?: Record<string, unknown>; registration?: Record<string, unknown>; login_state?: Record<string, unknown>; check?: Record<string, unknown>; sms_receiver_order?: SmsReceiverOrder; order?: SmsReceiverOrder; sms_url?: string; sms_line?: string; otp_source?: string };
export type WaConnectionState = LongConnectionState;
export type WaConnectionFilters = { login_state_id?: string; wa_account_id?: string; client_profile_id?: string; registered_identity_id?: string };
export type WaAccountProjection = WAAccount;
export type WaLatestContactsWindow = '1h' | '30m' | '5m' | 'unread';
export type WaLatestContactRow = { account?: WAAccount; contact?: WAContact };
export type WaLatestContactsResponse = { rows?: WaLatestContactRow[]; window?: WaLatestContactsWindow; since?: string; include_unread?: boolean; limit?: number };

export const waKeys = {
  accounts: () => ['wa', 'accounts'] as const,
  profiles: (waAccountId: string) => ['wa', 'profiles', waAccountId] as const,
  messages: (waAccountId: string, contactRef = '') => ['wa', 'messages', waAccountId, contactRef] as const,
  contacts: (waAccountId: string) => ['wa', 'contacts', waAccountId] as const,
  latestContacts: (window: WaLatestContactsWindow = '1h', includeUnread = true, limit = 80, systemOnly = false) => ['wa', 'latest-contacts', window, includeUnread ? '1' : '0', String(limit), systemOnly ? 'updates' : 'chats'] as const,
  contactResolve: (waAccountId: string) => ['wa', 'contacts', 'resolve', waAccountId] as const,
  registrationProgress: (waAccountId: string) => ['wa', 'registration-progress', waAccountId] as const,
  twoFactorStatus: (waAccountId: string) => ['wa', '2fa-status', waAccountId] as const,
  otpMessages: (waAccountId: string) => ['wa', 'otp-messages', waAccountId] as const,
  smsCodeLink: (waAccountId: string) => ['wa', 'sms-code-link', waAccountId] as const,
  connections: (filters: WaConnectionFilters = {}) => ['wa', 'connections', filters.login_state_id || '', filters.wa_account_id || '', filters.client_profile_id || '', filters.registered_identity_id || ''] as const,
};

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(path, { ...init, credentials: 'same-origin', headers: { 'Content-Type': 'application/json', ...(init?.headers || {}) } });
  if (resp.status === 401) redirectToLogin();
  if (!resp.ok) throw new Error(await apiErrorMessage(path, resp));
  if (isWaAPIPath(path) && !responseIsJSON(resp)) throw new Error(`WA 后端接口响应不是 JSON：${path}。请确认正在访问 Dashboard 后端地址，或 Vite 已代理 /api。`);
  return resp.json() as Promise<T>;
}

async function apiErrorMessage(path: string, resp: Response) {
  const detail = await apiErrorDetail(resp);
  if (resp.status === 404 && isWaAPIPath(path)) return `WA 后端接口未找到（HTTP 404）：${path}。请确认 wa-app Dashboard 后端已启动，或 Vite 已代理 /api。`;
  if (isWaAPIPath(path) && !detail && !responseIsJSON(resp)) return `WA 后端接口请求失败（HTTP ${resp.status}）：${path}。请确认 wa-app Dashboard 后端已启动。`;
  return detail ? `HTTP ${resp.status}: ${detail}` : `HTTP ${resp.status}`;
}

async function apiErrorDetail(resp: Response) {
  try {
    if (responseIsJSON(resp)) return apiErrorText(await resp.json());
    const text = (await resp.text()).replace(/\s+/g, ' ').trim();
    if (!text || text.startsWith('<!doctype') || text.startsWith('<html')) return '';
    return text.length > 160 ? `${text.slice(0, 160)}...` : text;
  } catch {
    return '';
  }
}

function apiErrorText(value: unknown) {
  if (!value || typeof value !== 'object') return '';
  const record = value as Record<string, unknown>;
  return firstString(record.error_message, record.message, nestedErrorMessage(record.error));
}

function nestedErrorMessage(value: unknown) {
  if (typeof value === 'string') return value;
  if (!value || typeof value !== 'object') return '';
  return firstString((value as Record<string, unknown>).message);
}

function firstString(...values: unknown[]) {
  for (const value of values) {
    if (typeof value === 'string' && value.trim()) return value.trim();
  }
  return '';
}

function responseIsJSON(resp: Response) {
  return (resp.headers.get('content-type') || '').toLowerCase().includes('application/json');
}

function isWaAPIPath(path: string) {
  return path.startsWith('/api/wa/');
}

function redirectToLogin() {
  const next = `${window.location.pathname}${window.location.search}${window.location.hash}`;
  window.location.assign(`/login?next=${encodeURIComponent(next || '/')}`);
}

export function getWaConnections(filters: WaConnectionFilters = {}) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(filters)) if (value) params.set(key, value);
  return getWaResponse<GetLongConnectionStatusResponse>(`/api/wa/long-connections${params.size ? `?${params}` : ''}`);
}

export async function getWaAccounts(cursor = '') {
  const accounts: WAAccount[] = [];
  const seenCursors = new Set<string>();
  let response: ListWAAccountsResponse;
  do {
    if (seenCursors.has(cursor)) throw new Error('账号分页游标重复，无法加载全部账号');
    seenCursors.add(cursor);
    const params = new URLSearchParams({ limit: String(ACCOUNT_PAGE_SIZE) });
    if (cursor) params.set('cursor', cursor);
    response = await getWaResponse<ListWAAccountsResponse>(`/api/wa/accounts?${params}`);
    if (Array.isArray(response.accounts)) accounts.push(...response.accounts.filter((account): account is WAAccount => Boolean(account)));
    cursor = response.next_cursor || '';
  } while (cursor);
  return { ...response, accounts, next_cursor: '' };
}

export function getWaAccountOtpMessages(waAccountId: string, options: { cursor?: string; limit?: number; includeSensitiveValues?: boolean } = {}) {
  const params = new URLSearchParams({ wa_account_id: waAccountId, limit: String(options.limit || 20) });
  if (options.cursor) params.set('cursor', options.cursor);
  if (options.includeSensitiveValues !== undefined) params.set('include_sensitive_values', String(options.includeSensitiveValues));
  return getWaResponse<ListAccountOtpMessagesResponse>(`/api/wa/account-otp-messages?${params}`);
}

export function getWaClientProfiles(waAccountId: string, cursor = '') {
  const params = new URLSearchParams({ wa_account_id: waAccountId, limit: '20' });
  if (cursor) params.set('cursor', cursor);
  return getWaResponse<ListClientProfilesResponse>(`/api/wa/client-profiles?${params}`);
}

export type MarkWaMessagesReadInput = { accountMessageIds?: string[]; contactRef?: string; localOnly?: boolean };

export function markWaMessagesRead(waAccountId: string, input: MarkWaMessagesReadInput) {
  return mutateWaResponse<MarkAccountMessagesReadResponse>('/api/wa/messages/read', { method: 'POST', body: JSON.stringify({ wa_account_id: waAccountId, account_message_ids: input.accountMessageIds || [], contact_ref: input.contactRef || '', local_only: Boolean(input.localOnly) }) });
}

export function deleteWaMessagesForMe(waAccountId: string, accountMessageIds: string[]) {
  return mutateWaResponse<DeleteAccountMessagesResponse>('/api/wa/messages/delete', { method: 'POST', body: JSON.stringify({ wa_account_id: waAccountId, account_message_ids: accountMessageIds, mode: 'for_me' }) });
}

export function sendWaTextMessage(waAccountId: string, contactRef: string, text: string) {
  return mutateWaResponse<SendTextMessageResponse>('/api/wa/messages/send', { method: 'POST', body: JSON.stringify({ wa_account_id: waAccountId, contact_ref: contactRef, text }) });
}

export function getWaMessages(waAccountId: string, contactRef: string, cursor = '') {
  const params = new URLSearchParams({ wa_account_id: waAccountId, contact_ref: contactRef, limit: '100', include_sensitive_text: 'true' });
  if (cursor) params.set('cursor', cursor);
  return getWaResponse<ListAccountMessagesResponse>(`/api/wa/messages?${params}`);
}

export function getWaContacts(waAccountId: string, cursor = '') {
  const params = new URLSearchParams({ wa_account_id: waAccountId, limit: '500' });
  if (cursor) params.set('cursor', cursor);
  return getWaResponse<ListWAContactsResponse>(`/api/wa/contacts?${params}`);
}

export function getWaLatestContacts(input: { window?: WaLatestContactsWindow; includeUnread?: boolean; limit?: number; systemOnly?: boolean } = {}) {
  const params = new URLSearchParams({
    window: input.window || '1h',
    include_unread: String(input.includeUnread ?? true),
    limit: String(input.limit || 80),
    system_only: String(input.systemOnly ?? false),
  });
  return api<WaLatestContactsResponse>(`/api/wa/latest-contacts?${params}`);
}

export function resolveWaContacts(waAccountId: string, jids: string[]) {
  return mutateWaResponse<ResolveWAContactsResponse>('/api/wa/contacts/resolve', { method: 'POST', body: JSON.stringify({ wa_account_id: waAccountId, jids, limit: jids.length }) });
}

export function deleteWaContact(waAccountId: string, contactID: string) {
  const params = new URLSearchParams({ wa_account_id: waAccountId });
  return mutateWaResponse<DeleteWAContactResponse>(`/api/wa/contacts/${encodeURIComponent(contactID)}?${params}`, { method: 'DELETE' });
}

export async function deleteWaAccount(account: WAAccount | string) {
  const accountID = typeof account === 'string' ? account : waAccountID(account);
  if (!accountID) throw new Error('wa_account_id is required');
  const resp = await api<DeleteWAAccountResponse>(`/api/wa/accounts/${encodeURIComponent(accountID)}`, { method: 'DELETE' });
  if (!resp.success || resp.error?.message) throw new Error(resp.error?.message || 'delete WAAccount failed');
  return resp;
}

export async function cleanupPendingRegistrationWaAccounts() {
  return mutateWaResponse<DeletePendingRegistrationWAAccountsResponse>('/api/wa/accounts/cleanup-pending-registration', { method: 'POST' });
}

export function getWaAccountRegistrationProgress(waAccountId: string) {
  const params = new URLSearchParams({ wa_account_id: waAccountId });
  return api<WaAccountRegistrationProgress>(`/api/wa/accounts/registration-progress?${params}`);
}

type RequestSignalOption = { signal?: AbortSignal };

export const probeWaPhoneSMS = (input: WaPhoneInput, options: RequestSignalOption = {}) => api<WaWorkflowResponse>('/api/wa/phone/sms-probe', { method: 'POST', body: JSON.stringify(input), signal: options.signal });
export const registerWaPhone = (input: WaPhoneInput, deliveryMethod: VerificationDeliveryMethod, smsReceiverOrder?: SmsReceiverOrder, options: RequestSignalOption = {}) => api<WaWorkflowResponse>('/api/wa/register', { method: 'POST', body: JSON.stringify({ ...input, delivery_method: deliveryMethod, sms_receiver_order: smsReceiverOrder }), signal: options.signal });
export const checkWaLoginState = (input: { login_state_id?: string; registered_identity_id?: string; wa_account_id?: string; client_profile_id?: string; remote_timeout_seconds?: number }) => api<WaWorkflowResponse>('/api/wa/login-state-check', { method: 'POST', body: JSON.stringify(input) });
export const allocateSmsReceiverOrder = (input: SmsReceiverAllocateInput = {}, options: RequestSignalOption = {}) => api<WaWorkflowResponse>('/api/wa/actions/sms-receiver/allocate', { method: 'POST', body: JSON.stringify({ sms_receiver: input }), signal: options.signal });
export const createWaAccountCodeLink = (input: { wa_account_id: string }) => api<WaWorkflowResponse>('/api/wa/actions/sms-receiver/code-link', { method: 'POST', body: JSON.stringify({ sms_receiver: input }) });
export const getWaAccountCodeLink = (input: { wa_account_id: string }) => api<WaWorkflowResponse>('/api/wa/actions/sms-receiver/code-link/status', { method: 'POST', body: JSON.stringify({ sms_receiver: input }) });
export const deleteSmsReceiverCodeLink = (input: { wa_account_id?: string; phone?: string; token?: string; sms_url?: string }) => api<{ success?: boolean; deleted?: boolean; error_message?: string }>('/api/wa/actions/sms-receiver/code-link/delete', { method: 'POST', body: JSON.stringify({ sms_receiver: input }) });
export const getSmsReceiverCountries = (input: Pick<SmsReceiverAllocateInput, 'provider' | 'providers' | 'service' | 'proxy'> = {}, options: RequestSignalOption = {}) => api<{ success?: boolean; countries?: SmsReceiverCountry[]; errors?: Record<string, string>; error_message?: string }>('/api/wa/actions/sms-receiver/countries', { method: 'POST', body: JSON.stringify({ sms_receiver: input }), signal: options.signal });
export const getSmsReceiverSettings = () => api<{ success?: boolean; settings?: SmsReceiverSettings; error_message?: string }>('/api/wa/actions/sms-receiver/settings', { method: 'POST', body: JSON.stringify({}) });
export const saveSmsReceiverSettings = (settings: SmsReceiverSettingsUpdate) => api<{ success?: boolean; settings?: SmsReceiverSettings; error_message?: string }>('/api/wa/actions/sms-receiver/save-settings', { method: 'POST', body: JSON.stringify({ settings }) });
export const testSmsReceiverProxy = (input: { mode: 'direct' | 'proxy'; proxy?: string }) => api<SmsReceiverProxyTestResponse>('/api/wa/actions/sms-receiver/test-proxy', { method: 'POST', body: JSON.stringify({ sms_receiver: input }) });
export const querySmsReceiverBalance = (input: { provider: string; proxy?: string }) => api<SmsReceiverBalanceResponse>('/api/wa/actions/sms-receiver/balance', { method: 'POST', body: JSON.stringify({ sms_receiver: input }) });
export const cancelSmsReceiverOrder = (smsReceiverOrder: SmsReceiverOrder) => api<WaWorkflowResponse>('/api/wa/actions/sms-receiver/cancel', { method: 'POST', body: JSON.stringify({ sms_receiver_order: smsReceiverOrder }) });
export const finishSmsReceiverOrder = (smsReceiverOrder: SmsReceiverOrder) => api<WaWorkflowResponse>('/api/wa/actions/sms-receiver/finish', { method: 'POST', body: JSON.stringify({ sms_receiver_order: smsReceiverOrder }) });
export const getAutoSmsRegistration = () => api<AutoSmsRegistrationResponse>('/api/wa/auto-sms-registration');
export const startAutoSmsRegistration = (input: SmsReceiverAllocateInput = {}, targetCount = 0) => api<AutoSmsRegistrationResponse>('/api/wa/auto-sms-registration', { method: 'POST', body: JSON.stringify({ sms_receiver: input, timeout_seconds: input.timeoutSeconds || 180, target_count: targetCount }) });
export const stopAutoSmsRegistration = () => api<AutoSmsRegistrationResponse>('/api/wa/auto-sms-registration', { method: 'DELETE' });
export function getWaRuntimeLogs(afterID = '', limit = 100) {
  const params = new URLSearchParams({ limit: String(limit) });
  if (afterID) params.set('after_id', afterID);
  return api<WaRuntimeLogResponse>(`/api/wa/runtime-logs?${params}`);
}

export async function getWaTwoFactorAuthStatus(account: WAAccount, input: { remoteRefresh?: boolean } = {}) {
  const accountID = waAccountID(account);
  if (!accountID) throw new Error('wa_account_id is required');
  const params = new URLSearchParams({ wa_account_id: accountID });
  if (input.remoteRefresh) params.set('remote_refresh', 'true');
  return requireAccountSettingsResponse(await api<GetTwoFactorAuthStatusResponse>(`/api/wa/account-settings/2fa/status?${params}`));
}

export function submitWaRegistrationOTP(account: WAAccount | string, otp: string) {
  const accountID = typeof account === 'string' ? account : waAccountID(account);
  return api<WaWorkflowResponse>('/api/wa/actions/registration/resume-otp', { method: 'POST', body: JSON.stringify({ wa_account_id: accountID, otp }) });
}
export function receiveWaRegistrationSMSOTP(input: { waAccountID: string; verificationRequestID?: string; timeoutSeconds?: number }, options: RequestSignalOption = {}) {
  return api<WaWorkflowResponse>('/api/wa/actions/registration/receive-sms-otp', { method: 'POST', body: JSON.stringify({ wa_account_id: input.waAccountID, verification_request_id: input.verificationRequestID || '', timeout_seconds: input.timeoutSeconds || 180 }), signal: options.signal });
}
export function refreshWaAccountTransferChallenge(verificationRequestID: string) {
  return api<WaWorkflowResponse>('/api/wa/actions/registration/account-transfer/refresh', { method: 'POST', body: JSON.stringify({ verification_request_id: verificationRequestID }) });
}
export function pollWaAccountTransferRegistration(verificationRequestID: string, waAccountID = '', maxAttempts = 1) {
  return api<WaWorkflowResponse>('/api/wa/actions/registration/account-transfer/poll', { method: 'POST', body: JSON.stringify({ verification_request_id: verificationRequestID, wa_account_id: waAccountID, max_attempts: maxAttempts }) });
}

export async function setWaTwoFactorAuthSettings(account: WAAccount, pin: string) {
  return requireAccountSettingsResponse(await api<SetTwoFactorAuthSettingsResponse>('/api/wa/account-settings/2fa', { method: 'POST', body: JSON.stringify({ ...waAccountSettingsPayload(account), pin }) }));
}
export async function setWaAccountEmail(account: WAAccount, input: { email_address: string; google_id_token?: string }) {
  return requireAccountSettingsResponse(await api<SetAccountEmailResponse>('/api/wa/account-settings/email', { method: 'POST', body: JSON.stringify({ ...waAccountSettingsPayload(account), email_address: input.email_address, google_id_token: input.google_id_token || '' }) }));
}
export async function requestWaAccountEmailOtp(account: WAAccount) {
  return requireAccountSettingsResponse(await api<RequestAccountEmailOtpResponse>('/api/wa/account-settings/email/otp/request', { method: 'POST', body: JSON.stringify({ ...waAccountSettingsPayload(account), locale_language: 'en', locale_country: 'US' }) }));
}
export async function verifyWaAccountEmailOtp(account: WAAccount, code: string) {
  return requireAccountSettingsResponse(await api<VerifyAccountEmailOtpResponse>('/api/wa/account-settings/email/otp/verify', { method: 'POST', body: JSON.stringify({ ...waAccountSettingsPayload(account), code }) }));
}
export async function setWaAccountProfileName(account: WAAccount, displayName: string) {
  return requireAccountSettingsResponse(await api<SetAccountProfileNameResponse>('/api/wa/account-settings/profile/name', { method: 'POST', body: JSON.stringify({ selector: waAccountSettingsSelector(account), display_name: displayName }) }));
}
export async function setWaAccountProfilePicture(account: WAAccount, input: { image_base64: string; content_type: string }) {
  return requireAccountSettingsResponse(await api<SetAccountProfilePictureResponse>('/api/wa/account-settings/profile/picture', { method: 'POST', body: JSON.stringify({ selector: waAccountSettingsSelector(account), image: input.image_base64, content_type: input.content_type }) }));
}
export async function removeWaAccountProfilePicture(account: WAAccount) {
  return requireAccountSettingsResponse(await api<RemoveAccountProfilePictureResponse>('/api/wa/account-settings/profile/picture/remove', { method: 'POST', body: JSON.stringify({ selector: waAccountSettingsSelector(account) }) }));
}

export const waAccountID = (account?: WAAccount) => account?.wa_account_id || '';
export const waAccountTitle = (account?: WAAccount) => account?.display_name?.trim() || account?.phone?.e164_number || waAccountID(account) || '-';
export function waAccountProfilePictureURL(account: WAAccount | string, version = 'latest') {
  const accountID = typeof account === 'string' ? account : waAccountID(account);
  return accountID ? `/api/wa/accounts/${encodeURIComponent(accountID)}/profile-picture?v=${encodeURIComponent(version)}` : '';
}

function waAccountSettingsPayload(account: WAAccount) {
  const accountID = waAccountID(account);
  if (!accountID) throw new Error('wa_account_id is required');
  return { wa_account_id: accountID };
}
function waAccountSettingsSelector(account: WAAccount) {
  const accountID = waAccountID(account);
  if (!accountID) throw new Error('wa_account_id is required');
  return { wa_account_id: accountID };
}
function requireAccountSettingsResponse<T extends { error?: { message?: string }; operation?: { error?: { message?: string } } }>(resp: T) {
  const message = resp.error?.message || resp.operation?.error?.message;
  if (message) throw new Error(message);
  return resp;
}
function getWaResponse<T extends { error?: { message?: string } }>(path: string) {
  return api<T>(path).then(requireWaResponse);
}
function mutateWaResponse<T extends { error?: { message?: string } }>(path: string, init: RequestInit) {
  return api<T>(path, init).then(requireWaResponse);
}
function requireWaResponse<T extends { error?: { message?: string } }>(resp: T) {
  if (resp.error?.message) throw new Error(resp.error.message);
  return resp;
}
