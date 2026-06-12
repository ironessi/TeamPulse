package notification

import (
	"redis-demo/internal/dao"
	"redis-demo/internal/model/entity"
	"testing"
	"time"

	_ "github.com/gogf/gf/contrib/nosql/redis/v2"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gctx"
)

func TestNotificationEventPublishReadAndAck(t *testing.T) {
	ctx := gctx.New()
	cleanNotificationEventStreamForTest(t)
	t.Cleanup(func() {
		cleanNotificationEventStreamForTest(t)
	})

	payload := RetryNotificationPayload{
		ReceiverId:       920001,
		ActorId:          920002,
		NotificationType: TypeTaskReminder,
		Content:          "stream publish read ack test",
		RelatedTaskId:    920003,
		RetryCount:       2,
	}

	messageId, err := PublishNotificationEvent(ctx, payload)
	if err != nil {
		t.Fatalf("publish notification event failed: %v", err)
	}
	if messageId == "" {
		t.Fatal("published message ID should not be empty")
	}

	message, err := ReadOneNotificationEvent(ctx, "stream-test-worker-1")
	if err != nil {
		t.Fatalf("read notification event failed: %v", err)
	}
	if message == nil {
		t.Fatal("read notification event should return a message")
	}
	if message.Id != messageId {
		t.Fatalf("unexpected message ID: got=%s want=%s", message.Id, messageId)
	}
	if message.Payload != payload {
		t.Fatalf("unexpected message payload: got=%+v want=%+v", message.Payload, payload)
	}

	pendingCount, err := GetNotificationEventPendingCount(ctx)
	if err != nil {
		t.Fatalf("get pending count failed: %v", err)
	}
	if pendingCount != 1 {
		t.Fatalf("pending count should be 1 after read, got %d", pendingCount)
	}

	if err := AckNotificationEvent(ctx, message.Id); err != nil {
		t.Fatalf("ack notification event failed: %v", err)
	}

	pendingCount, err = GetNotificationEventPendingCount(ctx)
	if err != nil {
		t.Fatalf("get pending count after ack failed: %v", err)
	}
	if pendingCount != 0 {
		t.Fatalf("pending count should be 0 after ack, got %d", pendingCount)
	}
}

func TestClaimOnePendingNotificationEvent(t *testing.T) {
	ctx := gctx.New()
	cleanNotificationEventStreamForTest(t)
	t.Cleanup(func() {
		cleanNotificationEventStreamForTest(t)
	})

	payload := RetryNotificationPayload{
		ReceiverId:       930001,
		ActorId:          930002,
		NotificationType: TypeTaskMentioned,
		Content:          "stream pending claim test",
		RelatedTaskId:    930003,
		RetryCount:       1,
	}

	messageId, err := PublishNotificationEvent(ctx, payload)
	if err != nil {
		t.Fatalf("publish notification event failed: %v", err)
	}

	message, err := ReadOneNotificationEvent(ctx, "stream-test-worker-old")
	if err != nil {
		t.Fatalf("read notification event failed: %v", err)
	}
	if message == nil || message.Id != messageId {
		t.Fatalf("unexpected originally read message: %+v", message)
	}

	// 等待消息的空闲时间超过 minIdle，模拟原消费者读取后未及时确认。
	time.Sleep(10 * time.Millisecond)

	claimed, err := ClaimOnePendingNotificationEvent(
		ctx,
		"stream-test-worker-new",
		time.Millisecond,
	)
	if err != nil {
		t.Fatalf("claim pending notification event failed: %v", err)
	}
	if claimed == nil {
		t.Fatal("claim should return the pending message")
	}
	if claimed.Id != messageId {
		t.Fatalf("unexpected claimed message ID: got=%s want=%s", claimed.Id, messageId)
	}
	if claimed.Payload != payload {
		t.Fatalf("unexpected claimed payload: got=%+v want=%+v", claimed.Payload, payload)
	}

	if err := AckNotificationEvent(ctx, claimed.Id); err != nil {
		t.Fatalf("ack claimed notification event failed: %v", err)
	}

	pendingCount, err := GetNotificationEventPendingCount(ctx)
	if err != nil {
		t.Fatalf("get pending count after claim ack failed: %v", err)
	}
	if pendingCount != 0 {
		t.Fatalf("pending count should be 0 after claimed message ack, got %d", pendingCount)
	}
}

func TestProcessOneNotificationEventCreatesNotificationAndAcks(t *testing.T) {
	ctx := gctx.New()
	cleanNotificationEventStreamForTest(t)

	payload := RetryNotificationPayload{
		ReceiverId:       940001,
		ActorId:          940002,
		NotificationType: TypeTaskReminder,
		Content:          "stream process business test",
		RelatedTaskId:    940003,
		RetryCount:       0,
	}

	t.Cleanup(func() {
		cleanNotificationEventStreamForTest(t)
		if _, err := dao.Notification.Ctx(ctx).Where("content", payload.Content).Delete(); err != nil {
			t.Fatalf("clean stream notification failed: %v", err)
		}
		if _, err := g.Redis().Del(ctx, notificationUnreadKey(payload.ReceiverId)); err != nil {
			t.Fatalf("clean stream unread set failed: %v", err)
		}
	})

	if _, err := PublishNotificationEvent(ctx, payload); err != nil {
		t.Fatalf("publish notification event failed: %v", err)
	}

	processed, err := ProcessOneNotificationEvent(ctx, "stream-business-worker")
	if err != nil {
		t.Fatalf("process notification event failed: %v", err)
	}
	if !processed {
		t.Fatal("process notification event should process one message")
	}

	var notification entity.Notification
	if err := dao.Notification.Ctx(ctx).Where("content", payload.Content).Scan(&notification); err != nil {
		t.Fatalf("query created notification failed: %v", err)
	}
	if notification.Id == 0 {
		t.Fatal("notification should be created after processing stream event")
	}
	if notification.ReceiverId != payload.ReceiverId ||
		notification.ActorId != payload.ActorId ||
		notification.Type != payload.NotificationType ||
		notification.RelatedTaskId != payload.RelatedTaskId ||
		notification.IsRead != 0 {
		t.Fatalf("unexpected notification record: %+v", notification)
	}

	isMember, err := g.Redis().SIsMember(ctx, notificationUnreadKey(payload.ReceiverId), notification.Id)
	if err != nil {
		t.Fatalf("check unread notification set failed: %v", err)
	}
	if isMember == 0 {
		t.Fatalf("notification ID %d should be in unread set", notification.Id)
	}

	pendingCount, err := GetNotificationEventPendingCount(ctx)
	if err != nil {
		t.Fatalf("get pending count after process failed: %v", err)
	}
	if pendingCount != 0 {
		t.Fatalf("pending count should be 0 after successful process, got %d", pendingCount)
	}
}

func TestProcessOneClaimedNotificationEventCreatesNotificationAndAcks(t *testing.T) {
	ctx := gctx.New()
	cleanNotificationEventStreamForTest(t)

	payload := RetryNotificationPayload{
		ReceiverId:       950001,
		ActorId:          950002,
		NotificationType: TypeTaskReminder,
		Content:          "stream claimed process business test",
		RelatedTaskId:    950003,
		RetryCount:       0,
	}

	t.Cleanup(func() {
		cleanNotificationEventStreamForTest(t)
		if _, err := dao.Notification.Ctx(ctx).Where("content", payload.Content).Delete(); err != nil {
			t.Fatalf("clean claimed stream notification failed: %v", err)
		}
		if _, err := g.Redis().Del(ctx, notificationUnreadKey(payload.ReceiverId)); err != nil {
			t.Fatalf("clean claimed stream unread set failed: %v", err)
		}
	})

	messageId, err := PublishNotificationEvent(ctx, payload)
	if err != nil {
		t.Fatalf("publish notification event failed: %v", err)
	}

	message, err := ReadOneNotificationEvent(ctx, "stream-claimed-old-worker")
	if err != nil {
		t.Fatalf("read notification event failed: %v", err)
	}
	if message == nil || message.Id != messageId {
		t.Fatalf("unexpected pending message before claim: %+v", message)
	}

	time.Sleep(10 * time.Millisecond)

	processed, err := ProcessOneClaimedNotificationEvent(
		ctx,
		"stream-claimed-new-worker",
		time.Millisecond,
	)
	if err != nil {
		t.Fatalf("process claimed notification event failed: %v", err)
	}
	if !processed {
		t.Fatal("claimed notification event should be processed")
	}

	var notification entity.Notification
	if err := dao.Notification.Ctx(ctx).Where("content", payload.Content).Scan(&notification); err != nil {
		t.Fatalf("query claimed stream notification failed: %v", err)
	}
	if notification.Id == 0 {
		t.Fatal("claimed stream notification should be created")
	}
	if notification.ReceiverId != payload.ReceiverId ||
		notification.ActorId != payload.ActorId ||
		notification.Type != payload.NotificationType ||
		notification.RelatedTaskId != payload.RelatedTaskId ||
		notification.IsRead != 0 {
		t.Fatalf("unexpected claimed stream notification record: %+v", notification)
	}

	isMember, err := g.Redis().SIsMember(ctx, notificationUnreadKey(payload.ReceiverId), notification.Id)
	if err != nil {
		t.Fatalf("check claimed stream unread set failed: %v", err)
	}
	if isMember == 0 {
		t.Fatalf("claimed stream notification ID %d should be in unread set", notification.Id)
	}

	pendingCount, err := GetNotificationEventPendingCount(ctx)
	if err != nil {
		t.Fatalf("get pending count after claimed process failed: %v", err)
	}
	if pendingCount != 0 {
		t.Fatalf("pending count should be 0 after claimed process, got %d", pendingCount)
	}
}

func cleanNotificationEventStreamForTest(t *testing.T) {
	t.Helper()

	if _, err := g.Redis().Del(gctx.New(), notificationEventStreamKey); err != nil {
		t.Fatalf("clean notification event stream failed: %v", err)
	}
}
