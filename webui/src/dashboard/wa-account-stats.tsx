import type { LongConnectionState } from '../proto/byte/v/forge/waapp/v1/messaging';
import type { WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { WAAccountStatus } from '../proto/byte/v/forge/waapp/v1/profile';
import { waAccountID } from './wa-api';
import { connectionView } from './wa-connection-dot';
import { isConnectionTransferredOut } from './wa-long-connection-badge';

export type WaAccountStats = {
  connecting: number;
  failed: number;
  normal: number;
  pending: number;
  total: number;
};

export function buildWaAccountStats(accounts: WAAccount[], connections?: Map<string, LongConnectionState>): WaAccountStats {
  let connecting = 0;
  let failed = 0;
  let normal = 0;
  let pending = 0;
  for (const account of accounts) {
    const connection = connections?.get(waAccountID(account));
    const { category } = connectionView(connection?.status, false, isConnectionTransferredOut(connection));
    if (category === 'normal') normal += 1;
    if (category === 'connecting') connecting += 1;
    if (category === 'failed') failed += 1;
    if (account.status === WAAccountStatus.WA_ACCOUNT_STATUS_PENDING_REGISTRATION) pending += 1;
  }
  return { connecting, failed, normal, pending, total: accounts.length };
}
