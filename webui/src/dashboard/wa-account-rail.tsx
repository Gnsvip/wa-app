import { useMemo, useState } from 'react';
import { Inbox, Info, Loader2, Plus } from 'lucide-react';
import { Link, NavLink, useLocation } from 'react-router';
import { Button } from '@/components/ui/button';
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty';
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarInput,
  SidebarMenuAction,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSkeleton,
  SidebarRail,
  SidebarTrigger,
  useSidebar,
} from '@/components/ui/sidebar';
import type { LongConnectionState } from '../proto/byte/v/forge/waapp/v1/messaging';
import type { WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { waAccountID } from './wa-api';
import { WaAccountAvatar } from './wa-account-avatar';
import { WaConnectionDot } from './wa-connection-dot';
import { buildWaAccountStats, type WaAccountStats } from './wa-account-stats';
import { WaPendingCleanupButton } from './wa-pending-cleanup-button';
import { WaThemeToggle } from './wa-theme-toggle';
import { waAccountPath, waChatsPath, waLatestAccountPath, waLatestMessagesPath } from './wa-route-paths';

type RailProps = { accountStats?: WaAccountStats; accounts: WAAccount[]; selectedID: string; avatarVersion: string; connections: Map<string, LongConnectionState>; loading: boolean; connectionsLoading: boolean; hasNextPage: boolean; loadingMore: boolean; cleaningPending: boolean; onLoadMore: () => void; onCleanupPending: () => Promise<void> };
type AccountItemProps = { account: WAAccount; selected: boolean; avatarVersion: string; connection?: LongConnectionState; loading: boolean };

const railButtonClass = 'grid! h-12 w-full grid-cols-[2.5rem_minmax(0,1fr)] items-center gap-2 overflow-hidden p-1! pr-8! transition-colors group-data-[collapsible=icon]:h-12! group-data-[collapsible=icon]:w-full! group-data-[collapsible=icon]:grid-cols-[2.5rem_minmax(0,1fr)] group-data-[collapsible=icon]:p-1! group-data-[collapsible=icon]:pr-1!';
const accountActionClass = 'top-1/2! right-2! size-7! -translate-y-1/2 rounded-lg group-data-[collapsible=icon]:!hidden';
const collapsedTextClass = 'overflow-hidden opacity-100 transition-[max-width,opacity] duration-200 ease-linear group-data-[collapsible=icon]:max-w-0 group-data-[collapsible=icon]:opacity-0 group-data-[collapsible=icon]:pointer-events-none';

export function WaAccountRail({ accountStats, accounts, selectedID, avatarVersion, connections, loading, connectionsLoading, hasNextPage, loadingMore, cleaningPending, onLoadMore, onCleanupPending }: RailProps) {
  const [query, setQuery] = useState('');
  const { state } = useSidebar();
  const expanded = state === 'expanded';
  const visibleAccounts = useFilteredAccounts(accounts, expanded ? query : '');
  const stats = accountStats || buildWaAccountStats(accounts, connections);
  return (
    <Sidebar collapsible="icon" aria-label="WA 账号" className="overflow-hidden">
      <SidebarHeader className="h-16 justify-center border-b border-sidebar-border">
        <RailHeader value={query} stats={stats} cleaningPending={cleaningPending} onChange={setQuery} onCleanupPending={onCleanupPending} />
      </SidebarHeader>
      <div className="shrink-0 border-b border-sidebar-border p-1">
        <SidebarMenu>
          <LatestMessagesItem accounts={accounts} selectedID={selectedID} />
        </SidebarMenu>
      </div>
      <SidebarContent>
        <SidebarGroup className="p-1">
          <SidebarGroupContent>
            <SidebarMenu>
              {loading ? <LoadingItems /> : null}
              {visibleAccounts.map((account) => {
                const id = waAccountID(account);
                return <AccountItem key={id} account={account} selected={id === selectedID} avatarVersion={avatarVersion} connection={connections.get(id)} loading={connectionsLoading} />;
              })}
            </SidebarMenu>
            {!loading && visibleAccounts.length === 0 ? <EmptyAccounts searching={query.trim() !== ''} /> : null}
            {expanded && hasNextPage ? <LoadMoreButton loading={loadingMore} onLoadMore={onLoadMore} /> : null}
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
      <SidebarFooter className="border-t border-sidebar-border p-1">
        <WaThemeToggle className="size-12 shrink-0" />
      </SidebarFooter>
      <SidebarRail className="after:bg-transparent hover:after:bg-transparent" aria-label={expanded ? '收起账号栏' : '展开账号栏'} title={expanded ? '收起账号栏' : '展开账号栏'} />
    </Sidebar>
  );
}

function RailHeader({ value, stats, cleaningPending, onChange, onCleanupPending }: { value: string; stats: WaAccountStats; cleaningPending: boolean; onChange: (value: string) => void; onCleanupPending: () => Promise<void> }) {
  return (
    <div className="grid gap-1 group-data-[collapsible=icon]:flex group-data-[collapsible=icon]:h-10 group-data-[collapsible=icon]:items-center group-data-[collapsible=icon]:justify-start">
      <div className="flex h-8 items-center gap-2">
        <SidebarInput className="h-8 group-data-[collapsible=icon]:hidden" value={value} onChange={(event) => onChange(event.target.value)} placeholder="搜索手机号" aria-label="搜索账号" />
        <WaPendingCleanupButton pendingCount={stats.pending} busy={cleaningPending} onCleanup={onCleanupPending} />
        <Button asChild size="icon" variant="ghost" className="size-8 group-data-[collapsible=icon]:hidden" title="添加账号" aria-label="添加账号"><Link to="/accounts/new"><Plus size={16} /></Link></Button>
        <SidebarTrigger className="shrink-0" aria-label="切换账号栏" title="切换账号栏" />
      </div>
      <AccountStatsLine stats={stats} />
    </div>
  );
}

function AccountStatsLine({ stats }: { stats: WaAccountStats }) {
  return (
    <div className="grid max-h-4 w-full min-w-0 grid-cols-[1.25fr_1fr_1fr_1fr] items-center gap-1 overflow-hidden px-1 text-[11px] leading-none text-muted-foreground opacity-100 transition-[max-height,opacity] duration-200 ease-linear group-data-[collapsible=icon]:max-h-0 group-data-[collapsible=icon]:opacity-0">
      <span className="min-w-0 truncate">账号 {stats.total}</span>
      <AccountStatDot className="bg-emerald-500" value={stats.normal} label="已连接" />
      <AccountStatDot className="bg-amber-500" value={stats.connecting} label="连接中" />
      <AccountStatDot className="bg-destructive" value={stats.failed} label="异常（连接失败或已转出）" />
    </div>
  );
}

function AccountStatDot({ className, value, label }: { className: string; value: number; label: string }) {
  return (
    <span className="inline-flex min-w-0 items-center justify-center gap-1 tabular-nums" title={`${label}：${value}`} aria-label={`${label}：${value}`}>
      <span className={`size-1.5 rounded-full ${className}`} />
      <span>{value}</span>
    </span>
  );
}

function LatestMessagesItem({ accounts, selectedID }: { accounts: WAAccount[]; selectedID: string }) {
  const location = useLocation();
  const active = location.pathname === waLatestMessagesPath() || new URLSearchParams(location.search).get('latest') === '1';
  const target = latestMessagesTarget(accounts, selectedID);
  return (
    <SidebarMenuItem>
      <SidebarMenuButton asChild size="lg" isActive={active} tooltip="最新消息" className={railButtonClass}>
        <NavLink to={target} title="最新消息" aria-label="最新消息">
          <span className="grid size-10 shrink-0 place-items-center rounded-xl bg-emerald-50 text-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-200">
            <Inbox size={18} />
          </span>
          <span className={`min-w-0 max-w-[11rem] flex-1 ${collapsedTextClass}`}>
            <span className="block truncate text-sm font-medium">最新消息</span>
            <span className="block truncate text-xs text-muted-foreground">按来信时间排序</span>
          </span>
        </NavLink>
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}

function latestMessagesTarget(accounts: WAAccount[], selectedID: string) {
  const accountID = selectedID || waAccountID(accounts[0]);
  return accountID ? waLatestAccountPath(accountID) : waLatestMessagesPath();
}

function AccountItem({ account, selected, avatarVersion, connection, loading }: AccountItemProps) {
  const id = waAccountID(account);
  const label = waAccountRailLabel(account);
  return (
    <SidebarMenuItem>
      <SidebarMenuButton asChild size="lg" isActive={selected} tooltip={label.tooltip} className={railButtonClass}>
        <NavLink to={waChatsPath(id)} title={label.tooltip} aria-label={label.tooltip}>
          <span className="relative shrink-0">
            <WaAccountAvatar account={account} version={avatarVersion} size="md" />
            <WaConnectionDot className="absolute bottom-0 right-0" connection={connection} loading={loading} />
          </span>
          <span className={`min-w-0 max-w-[11rem] flex-1 ${collapsedTextClass}`}>
            <span className="block truncate text-sm font-medium">{label.primary}</span>
            {label.subtitle ? <span className="block truncate font-mono text-xs text-muted-foreground">{label.subtitle}</span> : null}
          </span>
        </NavLink>
      </SidebarMenuButton>
      <SidebarMenuAction asChild showOnHover={!selected} className={accountActionClass}>
        <Link to={waAccountPath(id)} title="账号详情" aria-label={`${label.tooltip} 账号详情`}><Info /></Link>
      </SidebarMenuAction>
    </SidebarMenuItem>
  );
}

function LoadMoreButton({ loading, onLoadMore }: { loading: boolean; onLoadMore: () => void }) {
  return <Button className="mt-2 w-full" variant="outline" onClick={onLoadMore} disabled={loading}>{loading ? <Loader2 className="size-4 animate-spin" /> : null}加载更多账号</Button>;
}

function LoadingItems() {
  return <><SidebarMenuItem><SidebarMenuSkeleton showIcon /></SidebarMenuItem><SidebarMenuItem><SidebarMenuSkeleton showIcon /></SidebarMenuItem></>;
}

function EmptyAccounts({ searching }: { searching: boolean }) {
  return <Empty className="mt-4 border-0 p-4"><EmptyHeader><EmptyTitle>{searching ? '没有匹配账号' : '还没有账号'}</EmptyTitle><EmptyDescription>{searching ? '没有匹配的已加载账号' : '添加账号后会显示在这里'}</EmptyDescription></EmptyHeader></Empty>;
}

function useFilteredAccounts(accounts: WAAccount[], query: string) {
  return useMemo(() => {
    const normalized = normalizeQuery(query);
    if (!normalized) return accounts;
    return accounts.filter((account) => normalizeQuery(waAccountPhoneSearchText(account)).includes(normalized));
  }, [accounts, query]);
}

function waAccountPhone(account: WAAccount) {
  const phone = account.phone;
  const callingCode = phoneCallingCode(phone?.country_calling_code || '');
  const nationalNumber = (phone?.national_number || '').trim();
  if (callingCode && nationalNumber) return `${callingCode} ${nationalNumber}`;
  return formatE164Number(phone?.e164_number || '', callingCode);
}

function waAccountPhoneSearchText(account: WAAccount) {
  const phone = account.phone;
  return `${waAccountPhone(account)} ${phone?.e164_number || ''} ${phone?.national_number || ''}`;
}

function formatE164Number(value: string, callingCode: string) {
  value = value.trim();
  if (!value || !callingCode || !value.startsWith(callingCode)) return value;
  const nationalNumber = value.slice(callingCode.length).trim();
  return nationalNumber ? `${callingCode} ${nationalNumber}` : value;
}

function phoneCallingCode(value: string) {
  value = value.trim();
  if (!value) return '';
  return value.startsWith('+') ? value : `+${value}`;
}

function waAccountRailLabel(account: WAAccount) {
  const name = account.display_name?.trim() || '';
  const phone = waAccountPhone(account);
  const primary = name || phone || '未录入手机号';
  const subtitle = name && phone ? phone : '';
  return { primary, subtitle, tooltip: subtitle ? `${primary} · ${subtitle}` : primary };
}

function normalizeQuery(value: string) {
  return value.trim().toLowerCase().replace(/\s+/g, '');
}
