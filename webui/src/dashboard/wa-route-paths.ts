export function waAccountPath(accountID: string) {
  return `/accounts/${encodeURIComponent(accountID)}`;
}

export function waLatestMessagesPath() {
  return '/latest';
}

export function waLatestAccountPath(accountID: string) {
  return withLatestQuery(waChatsPath(accountID));
}

export function waLatestContactPath(accountID: string, contactID: string) {
  return withLatestQuery(waContactPath(accountID, contactID));
}

export function waChatsPath(accountID: string) {
  return `${waAccountPath(accountID)}/chats`;
}

export function waContactPath(accountID: string, contactID: string) {
  return `${waChatsPath(accountID)}/${encodeURIComponent(contactID)}`;
}

function withLatestQuery(path: string) {
  return `${path}${path.includes('?') ? '&' : '?'}latest=1`;
}
