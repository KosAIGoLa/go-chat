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
	offlinePusher := &fakePusher{online: map[string]bool{}}
	svc = NewService(routes).WithPusher(offlinePusher)
	offline := svc.Deliver(context.Background(), Task{TargetUserID: 1, Message: message.Message{ID: 1, ConversationID: 10, Seq: 7, SenderID: 9, CreatedAtMs: 123}})
	if !offline.Offline {
		t.Fatalf("expected offline result")
	}
	if len(offlinePusher.offlineCalls) != 1 || offlinePusher.offlineCalls[0] != 1 {
		t.Fatalf("expected offline push call for user 1, got %+v", offlinePusher.offlineCalls)
	}
	routes.Register(route.DeviceRoute{UserID: 1, DeviceID: "web", GatewayID: "gw", ConnID: "c"})
	online := svc.Deliver(context.Background(), Task{TargetUserID: 1, Message: message.Message{ID: 1}})
	if online.Offline || len(online.OnlineRoutes) != 1 {
		t.Fatalf("expected online route, got %+v", online)
	}
}

func TestDeliverPushesOnlineRoutes(t *testing.T) {
	routes := route.NewRegistry()
	routes.Register(route.DeviceRoute{UserID: 1, DeviceID: "ios", GatewayID: "gw", ConnID: "c1"})
	routes.Register(route.DeviceRoute{UserID: 1, DeviceID: "web", GatewayID: "gw", ConnID: "c2"})
	pusher := &fakePusher{online: map[string]bool{"1:ios": true}}
	svc := NewService(routes).WithPusher(pusher)

	result := svc.Deliver(context.Background(), Task{TargetUserID: 1, Message: message.Message{ID: 99}})
	if result.Offline || len(result.RouteResults) != 2 {
		t.Fatalf("unexpected delivery result: %+v", result)
	}
	if len(pusher.calls) != 2 {
		t.Fatalf("expected two push attempts, got %+v", pusher.calls)
	}
	delivered := 0
	failed := 0
	for _, routeResult := range result.RouteResults {
		if routeResult.Delivered {
			delivered++
		} else {
			failed++
		}
	}
	if delivered != 1 || failed != 1 {
		t.Fatalf("expected one delivered and one failed route, got %+v", result.RouteResults)
	}
}

func TestDeliverWithoutPusherRecordsUndeliveredRoutes(t *testing.T) {
	routes := route.NewRegistry()
	routes.Register(route.DeviceRoute{UserID: 2, DeviceID: "ios", GatewayID: "gw", ConnID: "c1"})
	svc := NewService(routes)
	result := svc.Deliver(context.Background(), Task{TargetUserID: 2, Message: message.Message{ID: 100}})
	if result.Offline || len(result.RouteResults) != 1 || result.RouteResults[0].Delivered {
		t.Fatalf("expected online but undelivered route without pusher, got %+v", result)
	}
}

type fakePusher struct {
	online       map[string]bool
	calls        []string
	offlineCalls []uint64
}

func (p *fakePusher) PushToDevice(userID uint64, deviceID string, _ message.Message) bool {
	key := routeKey(userID, deviceID)
	p.calls = append(p.calls, key)
	return p.online[key]
}

func (p *fakePusher) PushOffline(userID uint64, _ message.Message) {
	p.offlineCalls = append(p.offlineCalls, userID)
}

func routeKey(userID uint64, deviceID string) string {
	return string(rune('0'+userID)) + ":" + deviceID
}
