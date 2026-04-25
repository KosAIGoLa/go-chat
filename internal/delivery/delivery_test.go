package delivery

import (
	"context"
	"testing"

	"github.com/ck-chat/ck-chat/internal/message"
	"github.com/ck-chat/ck-chat/internal/route"
)

func TestDeliverDetectsOfflineAndOnline(t *testing.T) {
	routes := route.NewRegistry()
	svc := NewService(routes)
	offline := svc.Deliver(context.Background(), Task{TargetUserID: 1, Message: message.Message{ID: 1}})
	if !offline.Offline {
		t.Fatalf("expected offline result")
	}
	routes.Register(route.DeviceRoute{UserID: 1, DeviceID: "web", GatewayID: "gw", ConnID: "c"})
	online := svc.Deliver(context.Background(), Task{TargetUserID: 1, Message: message.Message{ID: 1}})
	if online.Offline || len(online.OnlineRoutes) != 1 {
		t.Fatalf("expected online route, got %+v", online)
	}
}
