package fanout

import (
	"context"
	"testing"

	"github.com/ck-chat/ck-chat/internal/delivery"
	"github.com/ck-chat/ck-chat/internal/inbox"
	"github.com/ck-chat/ck-chat/internal/message"
	"github.com/ck-chat/ck-chat/internal/route"
)

func TestFanoutWritesInboxAndDeliversOnlineRoutes(t *testing.T) {
	routes := route.NewRegistry()
	routes.Register(route.DeviceRoute{UserID: 2, DeviceID: "ios", GatewayID: "gw-1", ConnID: "c-1"})
	inboxStore := inbox.NewStore()
	deliverySvc := delivery.NewService(routes)
	fanoutSvc := NewService(StaticMembers{10: {1, 2, 3}}, inboxStore, deliverySvc)

	results := fanoutSvc.Fanout(context.Background(), message.Message{ID: 99, ConversationID: 10, Seq: 7, SenderID: 1})
	if len(results) != 2 {
		t.Fatalf("expected two target users, got %+v", results)
	}
	if len(inboxStore.List(2)) != 1 || len(inboxStore.List(3)) != 1 {
		t.Fatalf("expected inbox entries")
	}
	if !results[1].Offline {
		t.Fatalf("user 3 should be offline: %+v", results)
	}
	if len(results[0].OnlineRoutes) != 1 {
		t.Fatalf("user 2 should have online route: %+v", results)
	}
}
