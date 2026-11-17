import { useMemo, useState } from 'react';
import type { CSSProperties, ReactNode } from 'react';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, Navigate, NavLink, Outlet, useMatches, useNavigate, useOutletContext, useParams } from 'react-router';
import { toast } from 'sonner';
import { Inbox, Info, Loader2, Plus } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty';
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar';
import { Toaster } from '@/components/ui/sonner';
import type { LongConnectionState } from '../proto/byte/v/forge/waapp/v1/messaging';
import type { WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { cleanupPendingRegistrationWaAccounts, deleteWaAccount, getWaAccounts, getWaClientProfiles, waAccountID, waAccountTitle, waKeys } from './wa-api';
import { WaAccountAdd } from './wa-account-add';
import { WaAccountAvatar } from './wa-account-avatar';
import { WaAccountInfoPage } from './wa-account-info-page';
import { WaAccountRail } from './wa-account-rail';
import { buildWaAccountStats, type WaAccountStats } from './wa-account-stats';
import { WhatsAppIcon } from './wa-brand-icon';
import { WaConnectionDot } from './wa-connection-dot';
import { WaInbox } from './wa-inbox';
import { useWaLongConnectionIndex } from './wa-long-connection-badge';
import { WaPendingCleanupButton } from './wa-pending-cleanup-button';
import { WaRuntimeLogPanel } from './wa-runtime-log-panel';
import { WaThemeToggle } from './wa-theme-toggle';
import { waAccountPath, waChatsPath, waLatestAccountPath } from './wa-route-paths';

type WaRouteContext = { accounts: WAAccount[]; accountsLoading: boolean; connections: Map<string, LongConnectionState>; deleting: boolean; refreshAccounts: () => Promise<void>; refreshAccountAvatars: () => void; deleteAccount: (account: WAAccount) => void; done: (message: string) => void; error: (message: string) => void };

const emptyAccounts: WAAccount[] = [];
const accountSidebarStyle = { '--sidebar-width': '15rem', '--sidebar-width-icon': '4rem' } as CSSProperties;

export function WaLayout() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [accountAvatarVersion, setAccountAvatarVersion] = useState(() => String(Date.now()));
  const [accountRailExpanded, setAccountRailExpanded] = useState(false);
  const accountsQuery = useInfiniteQuery({ queryKey: waKeys.accounts(), queryFn: ({ pageParam }) => getWaAccounts(pageParam), initialPageParam: '', getNextPageParam: (lastPage) => lastPage.next_cursor || undefined, refetchInterval: 10000 });
  const connections = useWaLongConnectionIndex();
  const accounts = useMemo(() => accountsQuery.data?.pages.flatMap((page) => page.accounts || []) || emptyAccounts, [accountsQuery.data]);
  const accountStats = useMemo(() => buildWaAccountStats(accounts, connections.byAccount), [accounts, connections.byAccount]);
  const selectedID = useSelectedAccountID(accounts);
  const deletion = useMutation({ mutationFn: deleteWaAccount, onSuccess: async () => { toast.success('账号已删除'); await refreshAccounts(); navigate('/', { replace: true }); }, onError: showErrorToast });
  const pendingCleanup = useMutation({ mutationFn: cleanupPendingRegistrationWaAccounts, onSuccess: async (resp) => { toast.success(`已清理 ${resp.deleted_count || 0} 个未完成注册账号`); await refreshAccounts(); }, onError: showErrorToast });
  async function refreshAccounts() {
    await queryClient.invalidateQueries({ queryKey: waKeys.accounts() });
  }
  const refreshAccountAvatars = () => setAccountAvatarVersion(String(Date.now()));
  const context: WaRouteContext = { accounts, accountsLoading: accountsQuery.isLoading, connections: connections.byAccount, deleting: deletion.isPending, refreshAccounts, refreshAccountAvatars, deleteAccount: deletion.mutate, done: toast.success, error: showErrorToast };
  return (
    <SidebarProvider open={accountRailExpanded} onOpenChange={setAccountRailExpanded} style={accountSidebarStyle} className="h-dvh min-h-0 overflow-hidden bg-background text-foreground">
      <WaAccountRail accountStats={accountStats} accounts={accounts} selectedID={selectedID} avatarVersion={accountAvatarVersion} connections={connections.byAccount} loading={accountsQuery.isLoading} connectionsLoading={connections.loading} hasNextPage={Boolean(accountsQuery.hasNextPage)} loadingMore={accountsQuery.isFetchingNextPage} cleaningPending={pendingCleanup.isPending} onLoadMore={() => { void accountsQuery.fetchNextPage(); }} onCleanupPending={async () => { await pendingCleanup.mutateAsync(); }} />
      <WaMobileAccountDock accountStats={accountStats} accounts={accounts} selectedID={selectedID} avatarVersion={accountAvatarVersion} connections={connections.byAccount} connectionsLoading={connections.loading} cleaningPending={pendingCleanup.isPending} onCleanupPending={async () => { await pendingCleanup.mutateAsync(); }} />
      <SidebarInset className="h-dvh min-w-0 overflow-hidden pl-16 md:pl-0"><Outlet context={context} /></SidebarInset>
      <Toaster richColors closeButton />
    </SidebarProvider>
  );
}

function WaMobileAccountDock({ accountStats, accounts, avatarVersion, cleaningPending, connections, connectionsLoading, onCleanupPending, selectedID }: { accountStats: WaAccountStats; accounts: WAAccount[]; avatarVersion: string; cleaningPending: boolean; connections: Map<string, LongConnectionState>; connectionsLoading: boolean; onCleanupPending: () => Promise<void>; selectedID: string }) {
  const latestTarget = latestMessagesTarget(accounts, selectedID);
  return (
    <nav className="fixed top-0 left-0 z-30 flex h-dvh w-16 flex-col border-r border-border bg-sidebar text-sidebar-foreground md:hidden" aria-label="WA 账号">
      <div className="flex shrink-0 flex-col items-center gap-1 border-b border-sidebar-border px-1 py-2">
        <WaPendingCleanupButton pendingCount={accountStats.pending} busy={cleaningPending} onCleanup={onCleanupPending} />
        <Button asChild size="icon" variant="ghost" className="size-9" title="添加账号" aria-label="添加账号">
          <Link to="/accounts/new"><Plus size={17} /></Link>
        </Button>
        <Button asChild size="icon" variant="ghost" className="size-9" title="最新消息" aria-label="最新消息">
          <Link to={latestTarget}><Inbox size={17} /></Link>
        </Button>
      </div>
      <div className="thin-scrollbar flex min-h-0 flex-1 flex-col items-center gap-2 overflow-y-auto px-1 py-3">
        {accounts.map((account) => {
          const id = waAccountID(account);
          const selected = id === selectedID;
          return (
            <div key={id} className="relative">
              <NavLink
                to={waChatsPath(id)}
                className={`relative grid size-11 place-items-center rounded-xl transition ${selected ? 'bg-sidebar-accent text-sidebar-accent-foreground' : 'hover:bg-sidebar-accent/70'}`}
                title={waAccountTitle(account)}
                aria-label={waAccountTitle(account)}
              >
                <span className="relative">
                  <WaAccountAvatar account={account} version={avatarVersion} size="md" />
                  <WaConnectionDot className="absolute bottom-0 right-0" connection={connections.get(id)} loading={connectionsLoading} />
                </span>
              </NavLink>
              <Link
                to={waAccountPath(id)}
                className="absolute -top-1 -right-1 grid size-5 place-items-center rounded-full border border-border bg-background text-foreground shadow-sm transition hover:bg-muted"
                title="账号详情"
                aria-label={`${waAccountTitle(account)} 账号详情`}
              >
                <Info size={12} />
              </Link>
            </div>
          );
        })}
      </div>
      <div className="flex shrink-0 justify-center border-t border-sidebar-border p-2 pb-[calc(0.5rem+env(safe-area-inset-bottom))]">
        <WaThemeToggle className="size-9" />
      </div>
    </nav>
  );
}

export function WaHomeRoute() {
  const { accounts, accountsLoading } = useWaContext();
  if (accountsLoading) return <PageCenter><LoadingText>加载账号...</LoadingText></PageCenter>;
  const firstID = waAccountID(accounts[0]);
  return firstID ? <Navigate to={waChatsPath(firstID)} replace /> : <NoAccount />;
}

export function WaCreateAccountRoute() {
  const { deleting, refreshAccounts, done, error } = useWaContext();
  return (
    <PageShell title="添加账号">
      <WaAccountAdd
        disabled={deleting}
        footer={<WaRuntimeLogPanel className="h-[220px] rounded-none border-0 bg-card sm:h-[260px]" />}
        onChanged={refreshAccounts}
        onDone={done}
        onError={error}
      />
    </PageShell>
  );
}

export function WaAccountInfoRoute() {
  const account = useRouteAccount();
  const { accounts, accountsLoading, deleting, deleteAccount, done, error, refreshAccounts, refreshAccountAvatars } = useWaContext();
  const accountID = waAccountID(account);
  const profilesQuery = useQuery({ queryKey: waKeys.profiles(accountID), queryFn: () => getWaClientProfiles(accountID), enabled: Boolean(accountID), refetchInterval: 30000 });
  if (accountsLoading) return <PageCenter><LoadingText>加载账号...</LoadingText></PageCenter>;
  if (!account) return <AccountFallback accounts={accounts} />;
  return <WaAccountInfoPage account={account} profiles={profilesQuery.data?.client_profiles || []} profilesLoading={profilesQuery.isLoading} busy={deleting} onDelete={deleteAccount} onDone={done} onError={error} onAccountChanged={() => { void refreshAccounts(); }} onAvatarChanged={refreshAccountAvatars} />;
}

export function WaInboxRoute() {
  const { contactID = '' } = useParams();
  const account = useRouteAccount();
  const { accounts, accountsLoading } = useWaContext();
  if (accountsLoading) return <PageCenter><LoadingText>加载消息...</LoadingText></PageCenter>;
  if (!account) return <AccountFallback accounts={accounts} />;
  return <WaInbox account={account} contactID={contactID} />;
}

export function WaLatestMessagesRoute() {
  const { accounts, accountsLoading } = useWaContext();
  if (accountsLoading) return <PageCenter><LoadingText>加载最新消息...</LoadingText></PageCenter>;
  if (accounts.length === 0) return <NoAccount />;
  return <Navigate to={waLatestAccountPath(waAccountID(accounts[0]))} replace />;
}

function useRouteAccount() {
  const { accountID = '' } = useParams();
  const { accounts } = useWaContext();
  return useMemo(() => accounts.find((account) => waAccountID(account) === accountID), [accounts, accountID]);
}

function useWaContext() {
  return useOutletContext<WaRouteContext>();
}

function useSelectedAccountID(accounts: WAAccount[]) {
  const accountID = [...useMatches()].reverse().find((match) => match.params.accountID)?.params.accountID || '';
  return accounts.some((account) => waAccountID(account) === accountID) ? accountID : '';
}

function latestMessagesTarget(accounts: WAAccount[], selectedID: string) {
  const accountID = selectedID || waAccountID(accounts[0]);
  return accountID ? waLatestAccountPath(accountID) : '/latest';
}

function AccountFallback({ accounts }: { accounts: WAAccount[] }) {
  const firstID = waAccountID(accounts[0]);
  return firstID ? <Navigate to={waChatsPath(firstID)} replace /> : <NoAccount />;
}

function NoAccount() {
  const navigate = useNavigate();
  return (
    <PageCenter>
      <Empty className="max-w-sm border-0">
        <EmptyHeader>
          <EmptyMedia><WhatsAppIcon className="size-12" /></EmptyMedia>
          <EmptyTitle>还没有账号</EmptyTitle>
          <EmptyDescription>添加账号后即可查看联系人和消息。</EmptyDescription>
        </EmptyHeader>
        <EmptyContent><Button onClick={() => navigate('/accounts/new')}><Plus size={16} />添加账号</Button></EmptyContent>
      </Empty>
    </PageCenter>
  );
}

function PageShell({ title, children }: { title: string; children: ReactNode }) {
  return <section className="grid h-dvh min-w-0 grid-rows-[auto_1fr] bg-background"><header className="flex h-16 items-center border-b border-border bg-card px-4 sm:px-5"><h1 className="text-base font-semibold">{title}</h1></header><main className="min-h-0 min-w-0 overflow-auto p-3 sm:p-6"><div className="mx-auto min-w-[min(100%,820px)] max-w-[1220px]">{children}</div></main></section>;
}

function PageCenter({ children }: { children: ReactNode }) {
  return <section className="grid h-dvh place-items-center bg-background p-8">{children}</section>;
}

function LoadingText({ children }: { children: ReactNode }) {
  return <span className="inline-flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />{children}</span>;
}

function showErrorToast(error: unknown) {
  toast.error(error instanceof Error ? error.message : String(error));
}

export function WaNotFoundRoute() {
  return <Navigate to="/" replace />;
}
