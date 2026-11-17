import assert from 'node:assert/strict';
import { test } from 'node:test';
import { renderToStaticMarkup } from 'react-dom/server';
import { MemoryRouter } from 'react-router';
import { WAContactKind, type WAContact } from '../proto/byte/v/forge/waapp/v1/contacts';
import { WaOtpSource, type OtpMessage } from '../proto/byte/v/forge/waapp/v1/extraction';
import { LongConnectionStatus, type LongConnectionState } from '../proto/byte/v/forge/waapp/v1/messaging';
import { WaErrorCode } from '../proto/byte/v/forge/waapp/v1/common';
import { WAAccountStatus, type WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { buildWaAccountStats } from './wa-account-stats';
import { buildWaContacts, isWaUpdateContact } from './wa-chat-model';
import { WaContactList } from './wa-contact-list';

test('connection counts match dots and pending registration stays independent', () => {
  const states = ([
    { status: LongConnectionStatus.LONG_CONNECTION_STATUS_CONNECTED },
    { status: LongConnectionStatus.LONG_CONNECTION_STATUS_HEARTBEAT_WAITING },
    { status: LongConnectionStatus.LONG_CONNECTION_STATUS_STARTING },
    { status: LongConnectionStatus.LONG_CONNECTION_STATUS_RECONNECTING },
    { status: LongConnectionStatus.LONG_CONNECTION_STATUS_FAILED },
    { status: LongConnectionStatus.LONG_CONNECTION_STATUS_STOPPED, last_error: { code: WaErrorCode.WA_ERROR_CODE_CONFLICT, message: '', retryable: false } },
    { status: LongConnectionStatus.LONG_CONNECTION_STATUS_STOPPED },
  ] satisfies Partial<LongConnectionState>[]).map(connectionFixture);
  const accounts = states.map((_, index) => accountFixture({ wa_account_id: String(index), status: WAAccountStatus.WA_ACCOUNT_STATUS_ACTIVE }));
  accounts.push(accountFixture({ wa_account_id: 'pending', status: WAAccountStatus.WA_ACCOUNT_STATUS_PENDING_REGISTRATION }));
  const connections = new Map(states.map((state, index) => [String(index), state]));
  assert.deepEqual(buildWaAccountStats(accounts, connections), { normal: 2, connecting: 2, failed: 2, pending: 1, total: 8 });
  assert.equal(buildWaAccountStats(accounts).failed, 0);
});

test('channels collapse without contributing to chat unread or hiding the OTP banner', () => {
  const contacts = buildWaContacts([], ([
    { jid: '9000@newsletter', kind: WAContactKind.WA_CONTACT_KIND_SYSTEM, display_name: '系统联系人 9000', unread_count: 900, last_message_preview: 'me**********…' },
    { jid: 'status@broadcast', kind: WAContactKind.WA_CONTACT_KIND_SYSTEM, unread_count: 100 },
    { jid: 'test@lid', kind: WAContactKind.WA_CONTACT_KIND_BUSINESS, display_name: '验证码服务', unread_count: 2, last_message_preview: 'Test verification message' },
    { jid: 'group@g.us', kind: WAContactKind.WA_CONTACT_KIND_GROUP, unread_count: 1 },
  ] satisfies Partial<WAContact>[]).map(contactFixture));
  assert.equal(contacts.filter(isWaUpdateContact).length, 2);
  const channel = contacts.find((contact) => contact.id === '9000@newsletter');
  assert.equal(channel?.title, '频道 9000');
  assert.equal(channel?.preview, '通知正文未解析');
  const html = renderToStaticMarkup(<MemoryRouter><WaContactList accountID="test-account" contacts={contacts} selectedID="" loading={false}
    otp={otpFixture} onOpenContact={() => {}} onDeleteContact={() => {}} /></MemoryRouter>);
  assert.match(html, /2 个聊天 · 3 条未读/);
  assert.match(html, /频道与系统通知（2）/);
  assert.match(html, /验证码服务/);
  assert.match(html, /fixture-only/);
  assert.match(html, /<details[^>]*>/);
  assert.doesNotMatch(html, /<details[^>]*\sopen[\s=>]/);
  assert.ok(html.indexOf('Test verification message') < html.indexOf('<details'));
  assert.ok(html.indexOf('频道 9000') > html.indexOf('<details'));
});

function connectionFixture(overrides: Partial<LongConnectionState>): LongConnectionState {
  return { login_state_id: '', wa_account_id: '', client_profile_id: '', registered_identity_id: '', message_session_id: '', status: LongConnectionStatus.LONG_CONNECTION_STATUS_UNSPECIFIED,
    heartbeat_supported: false, started_at: undefined, last_connected_at: undefined, last_heartbeat_at: undefined, last_message_at: undefined, reconnect_count: 0, last_error: undefined, ...overrides };
}

function accountFixture(overrides: Partial<WAAccount>): WAAccount {
  return { wa_account_id: '', display_name: '', phone: undefined, status: WAAccountStatus.WA_ACCOUNT_STATUS_UNSPECIFIED, audit: undefined, two_factor_auth: undefined, ...overrides };
}

function contactFixture(overrides: Partial<WAContact>): WAContact {
  return { contact_id: '', wa_account_id: '', jid: '', number: '', display_name: '', wa_name: '', verified_name: '', kind: WAContactKind.WA_CONTACT_KIND_UNSPECIFIED,
    is_whatsapp_user: false, is_reachable: false, audit: undefined, profile_picture_id: '', message_count: 0, unread_count: 0, last_message_at: undefined, last_message_preview: '', ...overrides };
}

const otpFixture: OtpMessage = { otp_message_id: '', wa_account_id: '', client_profile_id: '', registered_identity_id: '', message_id: '', candidate_id: '', source: WaOtpSource.WA_OTP_SOURCE_UNSPECIFIED,
  source_party: '', otp: { value: '', redacted_value: 'fixture-only', secret_ref: '' }, received_at: undefined, audit: undefined, expires_at: undefined };
