package notification

import (
	"encoding/json"
	"redis-demo/internal/dao"
	"strings"
	"testing"

	_ "github.com/gogf/gf/contrib/drivers/mysql/v2"
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

	queueLength, err := g.Redis().LLen(ctx, notificationRetryQueueKey)
	if err != nil {
		t.Fatalf("read retry queue length after dequeue failed: %v", err)
	}
	if queueLength != 0 {
		t.Fatalf("retry queue should be empty after two dequeues, got %d", queueLength)
	}

	processingLength, err := g.Redis().LLen(ctx, notificationRetryProcessingKey)
	if err != nil {
		t.Fatalf("read retry processing length failed: %v", err)
	}
	if processingLength != 2 {
		t.Fatalf("retry processing queue should contain two pending messages, got %d", processingLength)
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

func TestProcessOneNotificationRetryAcksProcessingMessageOnSuccess(t *testing.T) {
	ctx := gctx.New()
	cleanNotificationRetryQueueForTest(t)

	payload := RetryNotificationPayload{
		ReceiverId:       900001,
		ActorId:          900002,
		NotificationType: TypeTaskReminder,
		Content:          "retry ack success test",
		RelatedTaskId:    900003,
		RetryCount:       0,
	}

	if err := EnqueueNotificationRetry(ctx, payload); err != nil {
		t.Fatalf("enqueue retry failed: %v", err)
	}

	processed, err := ProcessOneNotificationRetry(ctx)
	if err != nil {
		t.Fatalf("process retry failed: %v", err)
	}
	if !processed {
		t.Fatalf("process retry should process one message")
	}

	queueLength, err := g.Redis().LLen(ctx, notificationRetryQueueKey)
	if err != nil {
		t.Fatalf("read retry queue length failed: %v", err)
	}
	if queueLength != 0 {
		t.Fatalf("retry queue should be empty after success, got %d", queueLength)
	}

	processingLength, err := g.Redis().LLen(ctx, notificationRetryProcessingKey)
	if err != nil {
		t.Fatalf("read retry processing length failed: %v", err)
	}
	if processingLength != 0 {
		t.Fatalf("processing queue should be acked after success, got %d", processingLength)
	}

	t.Cleanup(func() {
		cleanNotificationRetryQueueForTest(t)
		if _, err := dao.Notification.Ctx(ctx).Where("content", payload.Content).Delete(); err != nil {
			t.Fatalf("clean retry notification failed: %v", err)
		}
		if _, err := g.Redis().Del(ctx, notificationUnreadKey(payload.ReceiverId)); err != nil {
			t.Fatalf("clean retry unread set failed: %v", err)
		}
	})
}

func TestProcessOneNotificationRetryAcksProcessingMessageAndRequeuesOnFailure(t *testing.T) {
	ctx := gctx.New()
	cleanNotificationRetryQueueForTest(t)

	payload := RetryNotificationPayload{
		ReceiverId:       910001,
		ActorId:          910002,
		NotificationType: TypeTaskReminder,
		Content:          strings.Repeat("x", 300),
		RelatedTaskId:    910003,
		RetryCount:       0,
	}

	if err := EnqueueNotificationRetry(ctx, payload); err != nil {
		t.Fatalf("enqueue retry failed: %v", err)
	}

	processed, err := ProcessOneNotificationRetry(ctx)
	if err == nil {
		t.Fatalf("process retry should return insert error for oversized content")
	}
	if !processed {
		t.Fatalf("process retry should process one message")
	}

	processingLength, err := g.Redis().LLen(ctx, notificationRetryProcessingKey)
	if err != nil {
		t.Fatalf("read retry processing length failed: %v", err)
	}
	if processingLength != 0 {
		t.Fatalf("processing queue should be acked after failure, got %d", processingLength)
	}

	queueLength, err := g.Redis().LLen(ctx, notificationRetryQueueKey)
	if err != nil {
		t.Fatalf("read retry queue length failed: %v", err)
	}
	if queueLength != 1 {
		t.Fatalf("retry queue should contain one requeued message, got %d", queueLength)
	}

	values, err := g.Redis().Do(ctx, "LRANGE", notificationRetryQueueKey, 0, -1)
	if err != nil {
		t.Fatalf("read retry queue payload failed: %v", err)
	}
	rawValues := values.Vars()
	if len(rawValues) != 1 {
		t.Fatalf("retry queue should contain one raw payload, got %d", len(rawValues))
	}

	var requeued RetryNotificationPayload
	if err := json.Unmarshal([]byte(rawValues[0].String()), &requeued); err != nil {
		t.Fatalf("decode requeued payload failed: %v", err)
	}
	if requeued.RetryCount != 1 {
		t.Fatalf("requeued retry count should be 1, got %d", requeued.RetryCount)
	}

	t.Cleanup(func() {
		cleanNotificationRetryQueueForTest(t)
		if _, err := dao.Notification.Ctx(ctx).Where("content", payload.Content).Delete(); err != nil {
			t.Fatalf("clean retry notification failed: %v", err)
		}
		if _, err := g.Redis().Del(ctx, notificationUnreadKey(payload.ReceiverId)); err != nil {
			t.Fatalf("clean retry unread set failed: %v", err)
		}
	})
}

func cleanNotificationRetryQueueForTest(t *testing.T) {
	t.Helper()
	ctx := gctx.New()

	if _, err := g.Redis().Del(ctx, notificationRetryQueueKey, notificationRetryProcessingKey); err != nil {
		t.Fatalf("clean notification retry queue failed: %v", err)
	}
}
