package message

import (
	"context"
	"testing"

	apperrors "github.com/ck-chat/ck-chat/internal/errors"
	"github.com/ck-chat/ck-chat/internal/sequence"
)

func TestServiceSendIsIdempotent(t *testing.T) {
	svc := NewService(sequence.NewAllocator(), NewMemoryStore())
	req := SendRequest{ConversationID: 10, SenderID: 20, SenderDeviceID: "ios", ClientMsgID: "c1", Type: 1, Payload: []byte("hello")}
	first, err := svc.Send(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Send(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Message.ID != second.Message.ID || first.Message.Seq != second.Message.Seq {
		t.Fatalf("expected idempotent response, first=%+v second=%+v", first, second)
	}
}

func TestServiceSync(t *testing.T) {
	svc := NewService(sequence.NewAllocator(), NewMemoryStore())
	for _, id := range []string{"a", "b", "c"} {
		_, err := svc.Send(context.Background(), SendRequest{ConversationID: 10, SenderID: 20, SenderDeviceID: "ios", ClientMsgID: id, Type: 1})
		if err != nil {
			t.Fatal(err)
		}
	}
	messages, err := svc.Sync(context.Background(), 10, 2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].Seq != 2 || messages[1].Seq != 3 {
		t.Fatalf("unexpected messages: %+v", messages)
	}
}

func TestServiceRecall(t *testing.T) {
	svc := NewService(sequence.NewAllocator(), NewMemoryStore())
	resp, err := svc.Send(context.Background(), SendRequest{
		ConversationID: 10, SenderID: 20, SenderDeviceID: "ios", ClientMsgID: "r1", Type: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	recalled, err := svc.Recall(context.Background(), RecallRequest{
		MessageID: resp.Message.ID,
		SenderID:  20,
	})
	if err != nil {
		t.Fatalf("unexpected recall error: %v", err)
	}
	if recalled.Status != MessageStatusRecalled {
		t.Fatalf("expected recalled status, got %v", recalled.Status)
	}
	if recalled.RecalledAtMs == 0 {
		t.Fatal("expected non-zero RecalledAtMs")
	}

	// Verify sync returns the recalled message
	msgs, err := svc.Sync(context.Background(), 10, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected at least one message from sync")
	}
	if msgs[0].Status != MessageStatusRecalled {
		t.Fatalf("synced message should be recalled, got status=%v", msgs[0].Status)
	}
}

func TestServiceRecallRejectsNonSender(t *testing.T) {
	svc := NewService(sequence.NewAllocator(), NewMemoryStore())
	resp, err := svc.Send(context.Background(), SendRequest{
		ConversationID: 10, SenderID: 20, SenderDeviceID: "ios", ClientMsgID: "r2", Type: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.Recall(context.Background(), RecallRequest{
		MessageID: resp.Message.ID,
		SenderID:  21,
	})
	if err == nil {
		t.Fatal("expected error for non-sender recall")
	}
	appErr, ok := err.(apperrors.AppError)
	if !ok || appErr.Code != apperrors.MsgRecallNotAllowed {
		t.Fatalf("expected MsgRecallNotAllowed, got %v", err)
	}
}

func TestServiceRecallRequiresIDs(t *testing.T) {
	svc := NewService(sequence.NewAllocator(), NewMemoryStore())

	_, err := svc.Recall(context.Background(), RecallRequest{})
	if err == nil {
		t.Fatal("expected error for empty recall request")
	}
	appErr, ok := err.(apperrors.AppError)
	if !ok || appErr.Code != apperrors.SysBadRequest {
		t.Fatalf("expected SysBadRequest, got %v", err)
	}
}

func TestServiceDelete(t *testing.T) {
	svc := NewService(sequence.NewAllocator(), NewMemoryStore())
	resp, err := svc.Send(context.Background(), SendRequest{
		ConversationID: 10, SenderID: 20, SenderDeviceID: "ios", ClientMsgID: "d1", Type: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := svc.Delete(context.Background(), DeleteRequest{
		MessageID: resp.Message.ID,
		SenderID:  20,
	})
	if err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if deleted.Status != MessageStatusDeleted {
		t.Fatalf("expected deleted status, got %v", deleted.Status)
	}
	if deleted.RecalledAtMs == 0 {
		t.Fatal("expected non-zero deleted timestamp")
	}

	// Idempotent: delete again should return existing state
	deleted2, err := svc.Delete(context.Background(), DeleteRequest{
		MessageID: resp.Message.ID,
		SenderID:  20,
	})
	if err != nil {
		t.Fatalf("unexpected idempotent delete error: %v", err)
	}
	if deleted2.Status != MessageStatusDeleted {
		t.Fatalf("expected deleted status on idempotent call, got %v", deleted2.Status)
	}
}

func TestServiceDeleteRejectsNonSender(t *testing.T) {
	svc := NewService(sequence.NewAllocator(), NewMemoryStore())
	resp, err := svc.Send(context.Background(), SendRequest{
		ConversationID: 10, SenderID: 20, SenderDeviceID: "ios", ClientMsgID: "d2", Type: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.Delete(context.Background(), DeleteRequest{
		MessageID: resp.Message.ID,
		SenderID:  21,
	})
	if err == nil {
		t.Fatal("expected error for non-sender delete")
	}
	appErr, ok := err.(apperrors.AppError)
	if !ok || appErr.Code != apperrors.MsgDeleteNotAllowed {
		t.Fatalf("expected MsgDeleteNotAllowed, got %v", err)
	}
}

func TestServiceDeleteRequiresIDs(t *testing.T) {
	svc := NewService(sequence.NewAllocator(), NewMemoryStore())

	_, err := svc.Delete(context.Background(), DeleteRequest{})
	if err == nil {
		t.Fatal("expected error for empty delete request")
	}
	appErr, ok := err.(apperrors.AppError)
	if !ok || appErr.Code != apperrors.SysBadRequest {
		t.Fatalf("expected SysBadRequest, got %v", err)
	}
}

func TestServiceEdit(t *testing.T) {
	svc := NewService(sequence.NewAllocator(), NewMemoryStore())
	resp, err := svc.Send(context.Background(), SendRequest{
		ConversationID: 10, SenderID: 20, SenderDeviceID: "ios", ClientMsgID: "e1", Type: 1, Payload: []byte("original"),
	})
	if err != nil {
		t.Fatal(err)
	}

	edited, err := svc.Edit(context.Background(), EditRequest{
		MessageID: resp.Message.ID,
		SenderID:  20,
		Payload:   []byte("updated"),
	})
	if err != nil {
		t.Fatalf("unexpected edit error: %v", err)
	}
	if edited.Status != MessageStatusEdited {
		t.Fatalf("expected edited status, got %v", edited.Status)
	}
	if edited.EditedAtMs == 0 {
		t.Fatal("expected non-zero EditedAtMs")
	}
	if string(edited.Payload) != "updated" {
		t.Fatalf("expected payload 'updated', got %q", string(edited.Payload))
	}

	// Verify sync reflects the edit
	msgs, err := svc.Sync(context.Background(), 10, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected at least one message from sync")
	}
	if msgs[0].Status != MessageStatusEdited {
		t.Fatalf("synced message should be edited, got status=%v", msgs[0].Status)
	}
	if string(msgs[0].Payload) != "updated" {
		t.Fatalf("synced message payload should be 'updated', got %q", string(msgs[0].Payload))
	}
}

func TestServiceEditRejectsNonSender(t *testing.T) {
	svc := NewService(sequence.NewAllocator(), NewMemoryStore())
	resp, err := svc.Send(context.Background(), SendRequest{
		ConversationID: 10, SenderID: 20, SenderDeviceID: "ios", ClientMsgID: "e2", Type: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.Edit(context.Background(), EditRequest{
		MessageID: resp.Message.ID,
		SenderID:  21,
		Payload:   []byte("hacked"),
	})
	if err == nil {
		t.Fatal("expected error for non-sender edit")
	}
	appErr, ok := err.(apperrors.AppError)
	if !ok || appErr.Code != apperrors.MsgEditNotAllowed {
		t.Fatalf("expected MsgEditNotAllowed, got %v", err)
	}
}

func TestServiceEditRejectsRecalledMessage(t *testing.T) {
	svc := NewService(sequence.NewAllocator(), NewMemoryStore())
	resp, err := svc.Send(context.Background(), SendRequest{
		ConversationID: 10, SenderID: 20, SenderDeviceID: "ios", ClientMsgID: "e3", Type: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Recall(context.Background(), RecallRequest{MessageID: resp.Message.ID, SenderID: 20}); err != nil {
		t.Fatal(err)
	}

	_, err = svc.Edit(context.Background(), EditRequest{
		MessageID: resp.Message.ID,
		SenderID:  20,
		Payload:   []byte("edit recalled"),
	})
	if err == nil {
		t.Fatal("expected error editing recalled message")
	}
	appErr, ok := err.(apperrors.AppError)
	if !ok || appErr.Code != apperrors.MsgEditNotAllowed {
		t.Fatalf("expected MsgEditNotAllowed, got %v", err)
	}
}

func TestServiceEditRequiresIDs(t *testing.T) {
	svc := NewService(sequence.NewAllocator(), NewMemoryStore())

	_, err := svc.Edit(context.Background(), EditRequest{})
	if err == nil {
		t.Fatal("expected error for empty edit request")
	}
	appErr, ok := err.(apperrors.AppError)
	if !ok || appErr.Code != apperrors.SysBadRequest {
		t.Fatalf("expected SysBadRequest, got %v", err)
	}
}
