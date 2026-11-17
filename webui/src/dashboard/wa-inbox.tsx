import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Navigate, useSearchParams } from 'react-router';
import type { OtpMessage } from '../proto/byte/v/forge/waapp/v1/extraction';
import type { WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { Button } from '@/components/ui/button';
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { useIsMobile } from '@/hooks/use-mobile';
import { deleteWaContact, getWaAccountOtpMessages, getWaContacts, getWaMessages, markWaMessagesRead, sendWaTextMessage, waAccountID, waKeys } from './wa-api';
import { useWaContactAutoResolve } from './wa-contact-resolve';
import { buildWaChatEvents, buildWaContacts, isWaUpdateContact } from './wa-chat-model';
import { WaChatThread } from './wa-chat-thread';
import { WaContactList } from './wa-contact-list';
import { WaLatestMessagesPanel } from './wa-latest-messages';
import { waContactPath, waLatestAccountPath, waLatestContactPath } from './wa-route-paths';

type MarkReadInput = { contactID?: string };

export function WaInbox({ account, contactID }: { account: WAAccount; contactID: string }) {
  const [searchParams] = useSearchParams();
  const [deleteContactID, setDeleteContactID] = useState('');
  const isMobile = useIsMobile();
  const latestOpen = searchParams.get('latest') === '1';
  const accountID = waAccountID(account);
  const queryClient = useQueryClient();
  const contactsQuery = useQuery({ queryKey: waKeys.contacts(accountID), queryFn: () => getWaContacts(accountID), enabled: Boolean(accountID), refetchInterval: 30000 });
  useWaContactAutoResolve(accountID, contactsQuery.data?.contacts || []);
  const baseContacts = useMemo(() => buildWaContacts([], contactsQuery.data?.contacts || []), [contactsQuery.data?.contacts]);
  const routeContactID = contactID.trim();
  const routeContactKnown = baseContacts.some((contact) => contact.id === routeContactID);
  const keepRouteContact = Boolean(routeContactID && (latestOpen || routeContactKnown || contactsQuery.isLoading || contactsQuery.isFetching));
  const fallbackContactID = !latestOpen || !isMobile ? baseContacts.find((contact) => !isWaUpdateContact(contact))?.id || '' : '';
  const activeContactID = keepRouteContact ? routeContactID : fallbackContactID;
  const messagesQuery = useQuery({ queryKey: waKeys.messages(accountID, activeContactID), queryFn: () => getWaMessages(accountID, activeContactID), enabled: Boolean(accountID && activeContactID), refetchInterval: 8000 });
  const otpQuery = useQuery({ queryKey: waKeys.otpMessages(accountID), queryFn: () => getWaAccountOtpMessages(accountID, { limit: 5, includeSensitiveValues: true }), enabled: Boolean(accountID), refetchInterval: 5000 });
  const events = useMemo(() => buildWaChatEvents(messagesQuery.data?.messages || []), [messagesQuery.data?.messages]);
  const contacts = baseContacts;
  const eventContacts = useMemo(() => buildWaContacts(events), [events]);
  const activeContact = contacts.find((contact) => contact.id === activeContactID) || eventContacts.find((contact) => contact.id === activeContactID);
  const deleteTarget = contacts.find((contact) => contact.id === deleteContactID);
  const activeOtp = useMemo(() => latestActiveOtp(otpQuery.data?.otp_messages || []), [otpQuery.data?.otp_messages]);
  const threadEvents = events;
  const refreshMessageViews = async (messageContactID = activeContactID) => {
    await Promise.all([queryClient.invalidateQueries({ queryKey: waKeys.messages(accountID, messageContactID) }), queryClient.invalidateQueries({ queryKey: waKeys.contacts(accountID) }), queryClient.invalidateQueries({ queryKey: waKeys.otpMessages(accountID) })]);
  };
  const markReadMutation = useMutation({
    mutationFn: async (input: MarkReadInput) => {
      const resp = await markWaMessagesRead(accountID, { contactRef: input.contactID });
      if (resp.error?.message) throw new Error(resp.error.message);
      return resp;
    },
    onSettled: (_data, _error, input) => refreshMessageViews(input.contactID || activeContactID),
  });
  const deleteContactMutation = useMutation({
    mutationFn: async (deleteContactID: string) => {
      const resp = await deleteWaContact(accountID, deleteContactID);
      if (resp.error?.message) throw new Error(resp.error.message);
      return resp;
    },
    onSettled: () => refreshMessageViews(),
  });
  const sendMutation = useMutation({
    mutationFn: async (text: string) => {
      const resp = await sendWaTextMessage(accountID, activeContactID, text);
      if (resp.error?.message) throw new Error(resp.error.message);
      return resp;
    },
    onSettled: () => refreshMessageViews(),
  });
  const error = messagesQuery.data?.error?.message || contactsQuery.data?.error?.message || otpQuery.data?.error?.message || mutationError(markReadMutation.error) || mutationError(deleteContactMutation.error) || mutationError(sendMutation.error);
  if (activeContactID && activeContactID !== routeContactID) return <Navigate to={latestOpen ? waLatestContactPath(accountID, activeContactID) : waContactPath(accountID, activeContactID)} replace />;
  const threadLoading = messagesQuery.isLoading || (contactsQuery.isLoading && !activeContact);
  const threadRefreshing = !threadLoading && (messagesQuery.isFetching || contactsQuery.isFetching);
  const layoutClass = latestOpen ? 'grid h-dvh min-h-0 bg-background md:grid-cols-[300px_300px_minmax(0,1fr)]' : 'grid h-dvh min-h-0 md:grid-cols-[320px_minmax(0,1fr)]';
  const latestPaneClass = activeContactID ? 'hidden min-h-0 md:contents' : 'min-h-0 md:contents';
  const contactPaneClass = latestOpen ? 'hidden min-h-0 md:contents' : 'contents';
  const threadPaneClass = latestOpen && !activeContactID ? 'hidden min-h-0 md:contents' : latestOpen ? 'min-h-0 md:contents' : 'contents';
  return (
    <>
      <section className={layoutClass}>
        {latestOpen ? <div className={latestPaneClass}><WaLatestMessagesPanel selectedAccountID={accountID} selectedContactID={activeContactID} /></div> : null}
        <div className={contactPaneClass}>
          <WaContactList accountID={accountID} contacts={contacts} selectedID={activeContactID} loading={contactsQuery.isLoading} error={error} deletingID={deleteContactMutation.variables} otp={activeOtp} preserveLatest={latestOpen} onOpenContact={(id) => openContact(id, markReadMutation.mutate)} onDeleteContact={setDeleteContactID} />
        </div>
        <div className={threadPaneClass}>
          <WaChatThread backHref={latestOpen ? waLatestAccountPath(accountID) : undefined} contact={activeContact} events={threadEvents} loading={threadLoading} refreshing={threadRefreshing} sending={sendMutation.isPending} error={error} onSendMessage={(text) => sendMutation.mutateAsync(text)} />
        </div>
      </section>
      <DeleteContactDialog
        busy={deleteContactMutation.isPending}
        contactTitle={deleteTarget?.title || ''}
        open={Boolean(deleteContactID)}
        onOpenChange={(open) => {
          if (!open && !deleteContactMutation.isPending) setDeleteContactID('');
        }}
        onDelete={() => {
          if (!deleteContactID) return;
          const targetID = deleteContactID;
          setDeleteContactID('');
          deleteContactMutation.mutate(targetID);
        }}
      />
    </>
  );
}

function openContact(contactID: string, mutate: (input: MarkReadInput) => void) {
  if (contactID) mutate({ contactID });
}

function mutationError(error: unknown) {
  return error instanceof Error ? error.message : '';
}

function DeleteContactDialog({ busy, contactTitle, onDelete, onOpenChange, open }: { busy: boolean; contactTitle: string; open: boolean; onOpenChange: (open: boolean) => void; onDelete: () => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>删除会话？</DialogTitle>
          <DialogDescription>
            只删除本地联系人和会话记录，不会删除 WA 账号。
          </DialogDescription>
        </DialogHeader>
        {contactTitle ? <p className="rounded-lg bg-muted px-3 py-2 text-sm text-muted-foreground">{contactTitle}</p> : null}
        <DialogFooter>
          <DialogClose asChild><Button variant="outline" disabled={busy}>取消</Button></DialogClose>
          <Button variant="destructive" disabled={busy} onClick={onDelete}>删除</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function latestActiveOtp(messages: OtpMessage[]) {
  const now = Date.now();
  return messages.find((message) => {
    const expiresAt = Date.parse(message.expires_at || '');
    const code = message.otp?.value || message.otp?.redacted_value || '';
    return Boolean(code) && Number.isFinite(expiresAt) && expiresAt > now;
  });
}
