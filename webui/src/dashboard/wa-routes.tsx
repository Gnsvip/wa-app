import { createBrowserRouter } from 'react-router';
import { WaAccountInfoRoute, WaCreateAccountRoute, WaHomeRoute, WaInboxRoute, WaLatestMessagesRoute, WaLayout, WaNotFoundRoute } from './wa-page';

export const waRouter = createBrowserRouter([
  {
    path: '/',
    Component: WaLayout,
    children: [
      { index: true, Component: WaHomeRoute },
      { path: 'latest', Component: WaLatestMessagesRoute },
      { path: 'latest/:latestAccountID/:latestContactID', Component: WaLatestMessagesRoute },
      { path: 'accounts/new', Component: WaCreateAccountRoute },
      {
        path: 'accounts/:accountID',
        children: [
          { index: true, Component: WaAccountInfoRoute },
          { path: 'chats', Component: WaInboxRoute },
          { path: 'chats/:contactID', Component: WaInboxRoute },
        ],
      },
      { path: '*', Component: WaNotFoundRoute },
    ],
  },
]);
