package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestLatestContactsSeparateUpdatesBeforeLimit(t *testing.T) {
	ctx := context.Background()
	s := maintenanceTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.SaveWAAccount(ctx, &waappv1.WAAccount{WaAccountId: "feed-account", Phone: &waappv1.PhoneTarget{E164Number: "+12025550100"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMessageSession(ctx, &waappv1.MessageSession{MessageSessionId: "feed-session", WaAccountId: "feed-account", OpenedAt: timestamppb.New(now)}); err != nil {
		t.Fatal(err)
	}
	add := func(ref string, at time.Time, read bool) {
		t.Helper()
		contact := contactFromRef("feed-account", ref, at, now)
		if err := s.SaveWAContacts(ctx, []*waappv1.WAContact{contact}); err != nil {
			t.Fatal(err)
		}
		message := &waappv1.InboundMessage{MessageId: "message-" + ref, MessageSessionId: "feed-session", ContactRef: ref, SenderRef: ref, Kind: waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE, ReceivedAt: timestamppb.New(at)}
		if read {
			message.ReadAt = timestamppb.New(now)
		}
		if err := s.SaveInboundMessages(ctx, []*waappv1.InboundMessage{message}); err != nil {
			t.Fatal(err)
		}
	}
	add("12025550101@s.whatsapp.net", now.Add(-2*time.Hour), false)
	add("12025550102@lid", now.Add(-time.Minute), true)
	add("12025550103@g.us", now.Add(-3*time.Hour), true)
	// More updates than the normal feed's page size must not crowd out chats.
	for i := 0; i < 90; i++ {
		add(fmt.Sprintf("%d@newsletter", 9000+i), now, false)
	}
	add("status@broadcast", now, false)
	for _, tc := range []struct {
		name   string
		filter LatestWAContactFilter
		want   int
	}{
		{"recent plus unread chats", LatestWAContactFilter{Since: now.Add(-time.Hour), IncludeUnread: true}, 2},
		{"recent chats only", LatestWAContactFilter{Since: now.Add(-time.Hour)}, 1},
		{"unread chats only", LatestWAContactFilter{UnreadOnly: true}, 1},
		{"all chats", LatestWAContactFilter{}, 3},
		{"limited chats", LatestWAContactFilter{Limit: 1}, 1},
		{"separate updates page", LatestWAContactFilter{SystemOnly: true}, 80},
		{"all updates", LatestWAContactFilter{SystemOnly: true, Limit: 200}, 91},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := s.ListLatestWAContacts(ctx, tc.filter)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != tc.want {
				t.Fatalf("rows=%d want=%d", len(rows), tc.want)
			}
			for _, row := range rows {
				isSystem := row.Contact.GetKind() == waappv1.WAContactKind_WA_CONTACT_KIND_SYSTEM
				if isSystem != tc.filter.SystemOnly {
					t.Fatal("contact returned in wrong feed")
				}
			}
		})
	}
	// Grouping is read-only: messages remain available in account history.
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM wa_sqlite_inbound_messages`).Scan(&count); err != nil || count != 94 {
		t.Fatalf("stored messages changed: count=%d err=%v", count, err)
	}
	contacts, _, err := s.ListWAContacts(ctx, "feed-account", "", 200)
	if err != nil || len(contacts) != 94 {
		t.Fatalf("account history unavailable: contacts=%d err=%v", len(contacts), err)
	}
}

func TestNewsletterFallbackDoesNotRenameCustomContacts(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"系统联系人 9000", "频道 9000"},
		{"", "频道 9000"},
		{"My channel", "My channel"},
	} {
		contact := &waappv1.WAContact{Jid: "9000@newsletter", Kind: waappv1.WAContactKind_WA_CONTACT_KIND_SYSTEM, DisplayName: tc.name}
		enrichWAContactFallback(contact)
		if contact.GetDisplayName() != tc.want {
			t.Errorf("name=%q want=%q", contact.GetDisplayName(), tc.want)
		}
	}
}
