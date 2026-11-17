import { useState } from 'react';
import { ArrowLeft, Trash2 } from 'lucide-react';
import { Link } from 'react-router';
import type { ClientProfile, WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { waAccountID, waAccountTitle } from './wa-api';
import { WaAccountAvatar } from './wa-account-avatar';
import { WaAccountDetail } from './wa-account-detail';
import { waChatsPath } from './wa-route-paths';
import { Button } from '@/components/ui/button';
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';

type Props = {
  account: WAAccount;
  profiles: ClientProfile[];
  profilesLoading: boolean;
  busy: boolean;
  onDelete: (account: WAAccount) => void;
  onDone: (message: string) => void;
  onError: (message: string) => void;
  onAccountChanged: () => void;
  onAvatarChanged: () => void;
};

export function WaAccountInfoPage({ account, profiles, profilesLoading, busy, onDelete, onDone, onError, onAccountChanged, onAvatarChanged }: Props) {
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState('');
  const subtitle = account.display_name ? account.phone?.e164_number || waAccountID(account) : '';
  const confirmToken = deleteConfirmToken(account);
  const canDelete = deleteConfirm.trim() === confirmToken;
  function closeDeleteDialog(open: boolean) {
    setDeleteOpen(open);
    if (!open) setDeleteConfirm('');
  }
  function confirmDelete() {
    if (!canDelete) return;
    closeDeleteDialog(false);
    onDelete(account);
  }
  return (
    <section className="grid h-dvh min-h-0 grid-rows-[auto_1fr] bg-background">
      <header className="flex h-16 items-center justify-between border-b border-border bg-card px-5">
        <div className="flex min-w-0 items-center gap-3">
          <Link className="inline-flex size-9 items-center justify-center rounded-lg transition hover:bg-muted" to={waChatsPath(waAccountID(account))} title="返回消息" aria-label="返回消息"><ArrowLeft size={16} /></Link>
          <WaAccountAvatar account={account} version={account.audit?.updated_at || 'latest'} size="md" />
          <div className="min-w-0"><h1 className="truncate text-base font-semibold">{waAccountTitle(account)}</h1>{subtitle ? <p className="truncate font-mono text-xs text-muted-foreground">{subtitle}</p> : null}</div>
        </div>
        <Dialog open={deleteOpen} onOpenChange={closeDeleteDialog}>
          <DialogTrigger asChild>
            <Button variant="destructive" size="icon" disabled={busy} title="删除账号" aria-label="删除账号"><Trash2 size={16} /></Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle><Trash2 size={16} />确认删除账号</DialogTitle>
              <DialogDescription>
                将删除本地账号资料、注册记录、短信记录、消息和登录态。这个操作不会删除 WhatsApp 服务端账号，但本地数据删除后不能从 Dashboard 恢复。
              </DialogDescription>
            </DialogHeader>
            <div className="grid gap-2">
              <p className="text-sm text-muted-foreground">请输入 <span className="font-mono text-foreground">{confirmToken}</span> 确认删除 {account.phone?.e164_number || waAccountID(account)}。</p>
              <Input value={deleteConfirm} onChange={(event) => setDeleteConfirm(event.target.value)} placeholder={`输入 ${confirmToken} 确认`} autoComplete="off" />
            </div>
            <DialogFooter>
              <DialogClose asChild><Button variant="outline" disabled={busy}>取消</Button></DialogClose>
              <Button variant="destructive" disabled={busy || !canDelete} onClick={confirmDelete}><Trash2 size={14} />确认删除</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </header>
      <main className="min-h-0 overflow-y-auto p-4">
        <div className="mx-auto max-w-6xl">
          <WaAccountDetail account={account} profiles={profiles} profilesLoading={profilesLoading} busy={busy} onDone={onDone} onError={onError} onAccountChanged={onAccountChanged} onAvatarChanged={onAvatarChanged} />
        </div>
      </main>
    </section>
  );
}

function deleteConfirmToken(account: WAAccount) {
  const digits = (account.phone?.e164_number || '').replace(/\D/g, '');
  return digits.slice(-4) || 'DELETE';
}
