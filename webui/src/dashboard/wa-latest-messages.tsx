import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Inbox, Loader2, Search } from 'lucide-react';
import { NavLink } from 'react-router';
import type { WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty';
import { Input } from '@/components/ui/input';
import { WaAccountAvatar } from './wa-account-avatar';
import { getWaLatestContacts, markWaMessagesRead, waAccountID, waAccountTitle, waKeys, type WaLatestContactRow, type WaLatestContactsWindow } from './wa-api';
import { buildWaContacts, formatChatTime, type WaContact } from './wa-chat-model';
import { WaContactAvatar } from './wa-contact-avatar';
import { waLatestContactPath } from './wa-route-paths';
import { WaUpdatesSection } from './wa-updates-section';

const latestContactsLimit = 80;
const latestWindowStorageKey = 'wa.latest-messages.window';
const latestWindowOptions: Array<{ value: WaLatestContactsWindow; label: string; title: string }> = [
  { value: '1h', label: '1小时', title: '显示最近 1 小时消息，外加未读消息' },
  { value: '30m', label: '30分', title: '显示最近 30 分钟消息，外加未读消息' },
  { value: '5m', label: '5分', title: '显示最近 5 分钟消息，外加未读消息' },
  { value: 'unread', label: '未读', title: '只显示未读消息' },
];

type LatestMessageRow = {
  account: WAAccount;
  accountID: string;
  accountLabel: string;
  accountSubtitle: string;
  contact: WaContact;
};

export function WaLatestMessagesPanel({ selectedAccountID, selectedContactID }: { selectedAccountID: string; selectedContactID: string }) {
  const [query, setQuery] = useState('');
  const [updatesOpen, setUpdatesOpen] = useState(false);
  const [latestWindow, setLatestWindow] = useState<WaLatestContactsWindow>(storedLatestWindow);
  const queryClient = useQueryClient();
  useEffect(() => {
    window.localStorage.setItem(latestWindowStorageKey, latestWindow);
  }, [latestWindow]);
  const latestQuery = useQuery({
    queryKey: waKeys.latestContacts(latestWindow, true, latestContactsLimit),
    queryFn: () => getWaLatestContacts({ window: latestWindow, includeUnread: true, limit: latestContactsLimit }),
    refetchInterval: 10000,
  });
  const rows = useMemo(() => latestRows(latestQuery.data?.rows || []), [latestQuery.data?.rows]);
  const updatesQuery = useQuery({
    queryKey: waKeys.latestContacts(latestWindow, true, latestContactsLimit, true),
    queryFn: () => getWaLatestContacts({ window: latestWindow, includeUnread: true, limit: latestContactsLimit, systemOnly: true }),
    enabled: updatesOpen,
    refetchInterval: updatesOpen ? 30000 : false,
  });
  const updateRows = useMemo(() => filterRows(latestRows(updatesQuery.data?.rows || []), query), [updatesQuery.data?.rows, query]);
  const visibleRows = useMemo(() => filterRows(rows, query), [rows, query]);
  const unreadCount = rows.reduce((sum, row) => sum + row.contact.unreadCount, 0);
  const contactsLoading = latestQuery.isLoading;
  const contactsFetching = latestQuery.isFetching;
  const contactError = latestQuery.error;

  const refreshActiveViews = async (row: LatestMessageRow) => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: waKeys.latestContacts(latestWindow, true, latestContactsLimit) }),
      queryClient.invalidateQueries({ queryKey: waKeys.latestContacts(latestWindow, true, latestContactsLimit, true) }),
      queryClient.invalidateQueries({ queryKey: waKeys.contacts(row.accountID) }),
      queryClient.invalidateQueries({ queryKey: waKeys.messages(row.accountID, row.contact.id) }),
    ]);
  };
  const markReadMutation = useMutation({
    mutationFn: (row: LatestMessageRow) => markWaMessagesRead(row.accountID, { contactRef: row.contact.id }),
    onSettled: (_data, _error, row) => {
      if (row) void refreshActiveViews(row);
    },
  });

  return (
    <aside className="grid h-full min-h-0 grid-rows-[auto_1fr] overflow-hidden border-r border-border bg-card">
      <div className="space-y-3 px-3 py-3">
        <div className="flex min-w-0 items-center justify-between gap-2">
          <div className="min-w-0">
            <h1 className="truncate text-base font-semibold">最新消息</h1>
            <p className="truncate text-xs text-muted-foreground">{rows.length} 个会话{unreadCount > 0 ? ` · ${unreadCount} 条未读` : ''}</p>
          </div>
          {contactsFetching ? <Loader2 className="size-4 shrink-0 animate-spin text-muted-foreground" /> : null}
        </div>
        <div className="grid grid-cols-4 gap-1">
          {latestWindowOptions.map((option) => (
            <Button
              key={option.value}
              type="button"
              variant={latestWindow === option.value ? 'default' : 'outline'}
              size="sm"
              className="h-8 px-1.5 text-xs"
              title={option.title}
              onClick={() => setLatestWindow(option.value)}
            >
              {option.label}
            </Button>
          ))}
        </div>
        <div className="relative">
          <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input className="pl-9" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索账号、联系人或消息" aria-label="搜索最新消息" />
        </div>
      </div>
      <div className="min-h-0 overflow-y-auto p-2">
        {contactError ? <div className="mb-2 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">{mutationError(contactError)}</div> : null}
        {contactsLoading ? <LatestLoading /> : null}
        {!contactsLoading && rows.length === 0 ? <LatestEmpty title="暂无最新消息" description={latestWindow === 'unread' ? '当前没有未读消息。' : '当前时间范围内没有新消息。'} /> : null}
        {!contactsLoading && rows.length > 0 && visibleRows.length === 0 ? <LatestEmpty title="没有匹配消息" description="换个关键词再试。" /> : null}
        {visibleRows.map((row) => (
          <LatestRow
            key={`${row.accountID}:${row.contact.id}`}
            row={row}
            selected={row.accountID === selectedAccountID && row.contact.id === selectedContactID}
            markingRead={markReadMutation.isPending && markReadMutation.variables?.accountID === row.accountID && markReadMutation.variables?.contact.id === row.contact.id}
            onOpen={() => {
              if (row.contact.unreadCount > 0) markReadMutation.mutate(row);
            }}
          />
        ))}
        <WaUpdatesSection onOpenChange={setUpdatesOpen} count={updatesQuery.data ? updateRows.length : undefined}>
          {updatesOpen && updatesQuery.isLoading ? <LatestLoading /> : null}
          {updatesQuery.error ? <p className="p-3 text-sm text-destructive">{mutationError(updatesQuery.error)}</p> : null}
          {updatesOpen && !updatesQuery.isLoading && !updatesQuery.error && updateRows.length === 0 ? <LatestEmpty title="暂无频道或系统通知" description="当前范围内没有匹配的通知。" /> : null}
          {updateRows.map((row) => (
            <LatestRow key={`${row.accountID}:${row.contact.id}`} row={row} selected={row.accountID === selectedAccountID && row.contact.id === selectedContactID}
              markingRead={markReadMutation.isPending && markReadMutation.variables?.accountID === row.accountID && markReadMutation.variables?.contact.id === row.contact.id}
              onOpen={() => { if (row.contact.unreadCount > 0) markReadMutation.mutate(row); }} />
          ))}
        </WaUpdatesSection>
      </div>
    </aside>
  );
}

function LatestRow({ markingRead, onOpen, row, selected }: { row: LatestMessageRow; selected: boolean; markingRead: boolean; onOpen: () => void }) {
  const unread = row.contact.unreadCount > 0;
  return (
    <Button asChild variant="ghost" className={`mb-1 h-auto w-full justify-start rounded-2xl p-0 text-left hover:bg-muted/70 ${selected ? 'bg-primary/10' : unread ? 'bg-emerald-50/70 dark:bg-emerald-950/20' : ''}`}>
      <NavLink to={waLatestContactPath(row.accountID, row.contact.id)} onClick={onOpen}>
        <span className="grid w-full min-w-0 grid-cols-[42px_1fr_auto] items-center gap-3 px-3 py-2">
          <span className="relative shrink-0">
            <WaContactAvatar contact={row.contact} />
            <span className="absolute -right-1 -bottom-1 rounded-full border border-background bg-background">
              <WaAccountAvatar account={row.account} version={row.account.audit?.updated_at || 'latest'} size="xs" />
            </span>
          </span>
          <span className="min-w-0 space-y-0.5">
            <span className="flex min-w-0 items-center gap-2">
              <span className={`truncate text-sm ${unread ? 'font-semibold text-foreground' : 'font-medium'}`}>{row.contact.title}</span>
              {unread ? <Badge>{row.contact.unreadCount}</Badge> : null}
            </span>
            <span className="block truncate text-xs text-muted-foreground">{row.accountLabel}{row.accountSubtitle ? ` · ${row.accountSubtitle}` : ''}</span>
            <span className={`block truncate text-xs ${unread ? 'font-medium text-foreground/85' : 'text-foreground/70'}`}>{row.contact.preview || row.contact.subtitle || '暂无消息预览'}</span>
          </span>
          <span className="grid shrink-0 justify-items-end gap-1 text-[11px] text-muted-foreground">
            <time>{formatChatTime(row.contact.lastAt)}</time>
            {markingRead ? <Loader2 className="size-3 animate-spin" /> : null}
          </span>
        </span>
      </NavLink>
    </Button>
  );
}

function LatestLoading() {
  return <div className="rounded-xl border bg-background px-3 py-8 text-center text-sm text-muted-foreground"><Loader2 className="mx-auto mb-2 size-4 animate-spin" />正在汇总最近消息</div>;
}

function LatestEmpty({ title, description }: { title: string; description: string }) {
  return (
    <Empty className="border-0 p-4">
      <EmptyHeader>
        <EmptyMedia><Inbox className="size-10" /></EmptyMedia>
        <EmptyTitle>{title}</EmptyTitle>
        <EmptyDescription>{description}</EmptyDescription>
      </EmptyHeader>
    </Empty>
  );
}

function latestRows(rawRows: WaLatestContactRow[]) {
  const rows: LatestMessageRow[] = [];
  for (const row of rawRows) {
    if (!row.account || !row.contact) continue;
    const account = row.account;
    const accountID = waAccountID(account);
    if (!accountID) continue;
    const contact = buildWaContacts([], [row.contact])[0];
    if (!contact || (!contact.preview && !contact.lastAt && contact.unreadCount <= 0)) continue;
    rows.push({ account, accountID, accountLabel: accountPhone(account) || waAccountTitle(account), accountSubtitle: account.display_name?.trim() || '', contact });
  }
  return rows.sort((left, right) => (right.contact.lastAt?.getTime() || 0) - (left.contact.lastAt?.getTime() || 0) || right.contact.unreadCount - left.contact.unreadCount);
}

function filterRows(rows: LatestMessageRow[], query: string) {
  const needle = query.trim().toLowerCase();
  if (!needle) return rows;
  return rows.filter((row) => `${row.accountLabel} ${row.accountSubtitle} ${row.contact.title} ${row.contact.subtitle} ${row.contact.preview} ${row.contact.id}`.toLowerCase().includes(needle));
}

function accountPhone(account: WAAccount) {
  const phone = account.phone;
  const e164 = phone?.e164_number?.trim() || '';
  if (e164) return e164;
  const calling = phone?.country_calling_code ? `+${phone.country_calling_code.replace(/^\+/, '')}` : '';
  const national = phone?.national_number?.trim() || '';
  return [calling, national].filter(Boolean).join(' ');
}

function mutationError(error: unknown) {
  return error instanceof Error ? error.message : error ? String(error) : '';
}

function storedLatestWindow(): WaLatestContactsWindow {
  const value = window.localStorage.getItem(latestWindowStorageKey);
  return isLatestWindow(value) ? value : '1h';
}

function isLatestWindow(value: string | null): value is WaLatestContactsWindow {
  return value === '1h' || value === '30m' || value === '5m' || value === 'unread';
}
