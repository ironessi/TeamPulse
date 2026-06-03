package notification

import (
	"testing"

	_ "github.com/gogf/gf/contrib/nosql/redis/v2"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gctx"
)

func TestNotificationRetryQueueEnqueueDequeueFIFO(t *testing.T) {
	ctx := gctx.New()
	cleanNotificationRetryQueueForTest(t)

	first := RetryNotificationPayload{
		ReceiverId:       1,
		ActorId:          2,
		NotificationType: TypeTaskCommented,
		Content:          "first retry",
		RelatedTaskId:    100,
		RetryCount:       0,
	}
	second := RetryNotificationPayload{
		ReceiverId:       3,
		ActorId:          4,
		NotificationType: TypeTaskReminder,
		Content:          "second retry",
		RelatedTaskId:    200,
		RetryCount:       1,
	}

	if err := EnqueueNotificationRetry(ctx, first); err != nil {
		t.Fatalf("enqueue first retry failed: %v", err)
	}
	if err := EnqueueNotificationRetry(ctx, second); err != nil {
		t.Fatalf("enqueue second retry failed: %v", err)
	}

	length, err := g.Redis().LLen(ctx, notificationRetryQueueKey)
	if err != nil {
		t.Fatalf("read retry queue length failed: %v", err)
	}
	if length != 2 {
		t.Fatalf("retry queue length should be 2, got %d", length)
	}

	gotFirst, err := DequeueNotificationRetry(ctx)
	if err != nil {
		t.Fatalf("dequeue first retry failed: %v", err)
	}
	if gotFirst == nil || *gotFirst != first {
		t.Fatalf("unexpected first payload: got=%+v want=%+v", gotFirst, first)
	}

	gotSecond, err := DequeueNotificationRetry(ctx)
	if err != nil {
		t.Fatalf("dequeue second retry failed: %v", err)
	}
	if gotSecond == nil || *gotSecond != second {
		t.Fatalf("unexpected second payload: got=%+v want=%+v", gotSecond, second)
	}

	empty, err := DequeueNotificationRetry(ctx)
	if err != nil {
		t.Fatalf("dequeue empty retry queue failed: %v", err)
	}
	if empty != nil {
		t.Fatalf("empty retry queue should return nil payload, got %+v", empty)
	}

	t.Cleanup(func() {
		cleanNotificationRetryQueueForTest(t)
	})
}

func TestNotificationRetryQueueEmptyReturnsNil(t *testing.T) {
	ctx := gctx.New()
	cleanNotificationRetryQueueForTest(t)

	payload, err := DequeueNotificationRetry(ctx)
	if err != nil {
		t.Fatalf("dequeue empty retry queue failed: %v", err)
	}
	if payload != nil {
		t.Fatalf("empty retry queue should return nil payload, got %+v", payload)
	}
}

func cleanNotificationRetryQueueForTest(t *testing.T) {
	t.Helper()
	ctx := gctx.New()

	if _, err := g.Redis().Del(ctx, notificationRetryQueueKey); err != nil {
		t.Fatalf("clean notification retry queue failed: %v", err)
	}
}
