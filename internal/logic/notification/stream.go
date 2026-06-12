package notification

import (
	"context"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
)

const (
	notificationEventStreamKey      = "notification:events"
	notificationEventGroupName      = "notification-workers"        //消费组
	notificationEventWorkerInterval = 5 * time.Second               //轮询间隔
	notificationEventConsumerName   = "notification-event-worker-1" //消费者名称
	notificationEventClaimMinIdle   = 30 * time.Second
)

// EnsureNotificationEventGroup 创建 Stream 消费组。
func EnsureNotificationEventGroup(ctx context.Context) error {
	// 1. 执行 XGROUP CREATE key group 0 MKSTREAM
	_, err := g.Redis().Do(ctx, "XGROUP", "CREATE", notificationEventStreamKey, notificationEventGroupName, "0", "MKSTREAM")
	if err == nil {
		return nil
	}
	// 2. 如果消费组已经存在，直接返回 nil
	if strings.Contains(err.Error(), "BUSYGROUP") {
		return nil
	}
	// 3. 返回其他错误
	return err
}

// PublishNotificationEvent 向 Stream 发布通知事件。
func PublishNotificationEvent(
	ctx context.Context,
	payload RetryNotificationPayload,
) (string, error) {
	// 1. 执行 XADD notification:events *
	value, err := g.Redis().Do(ctx, "XADD", notificationEventStreamKey, "*", "receiverId", payload.ReceiverId, "actorId", payload.ActorId, "content", payload.Content, "notificationType", payload.NotificationType, "relatedTaskId", payload.RelatedTaskId, "retryCount", payload.RetryCount)
	if err != nil {
		return "", err
	}

	// 3. 返回 Redis 自动生成的消息 ID
	return value.String(), nil
}

type NotificationEventMessage struct {
	Id      string
	Payload RetryNotificationPayload
}

// ReadOneNotificationEvent 使用消费组读取一条尚未分配的新消息。
// 读取成功后，消息会进入当前消费组的 pending 列表，等待后续 XACK。
func ReadOneNotificationEvent(
	ctx context.Context,
	consumerName string,
) (*NotificationEventMessage, error) {
	// 1. 校验 consumerName
	if consumerName == "" {
		return nil, gerror.New("consumerName 不能为空")
	}
	// 2. 确保消费组存在
	if err := EnsureNotificationEventGroup(ctx); err != nil {
		return nil, err
	}
	// 3. 使用 XREADGROUP 读取一条新消息
	// notification-workers = 消费组
	// worker-1             = 消费者名称
	// COUNT 1              = 最多读取一条
	// >                    = 只读取从未分配给消费者的新消息
	value, err := g.Redis().Do(ctx, "XREADGROUP", "GROUP", notificationEventGroupName, consumerName, "COUNT", 1, "STREAMS", notificationEventStreamKey, ">")
	if err != nil {
		return nil, err
	}
	// 4. 没有新消息时返回 nil, nil
	if value.IsNil() {
		return nil, nil
	}
	// XREADGROUP 返回结构：
	// [
	//   [streamKey, [
	//     [messageId, [field, value, field, value...]]
	//   ]]
	// ]
	// GoFrame 将 XREADGROUP 的结果转换为：
	// map[streamKey]messages
	streamMap := value.Map()

	rawMessages, exists := streamMap[notificationEventStreamKey]
	if !exists {
		return nil, nil
	}

	messages := g.NewVar(rawMessages).Vars()
	if len(messages) == 0 {
		return nil, nil
	}
	messageParts := messages[0].Vars()
	if len(messageParts) < 2 {
		return nil, gerror.New("Stream返回结构不完整")
	}
	messageId := messageParts[0].String()
	fields := messageParts[1].Vars()
	// 5. 解析 Stream、消息 ID 和字段列表
	fieldsMap := make(map[string]string)
	for i := 0; i+1 < len(fields); i += 2 {
		fieldsMap[fields[i].String()] = fields[i+1].String()
	}
	// 6. 将字段转换为 RetryNotificationPayload
	payload := RetryNotificationPayload{
		ReceiverId:       g.NewVar(fieldsMap["receiverId"]).Uint64(),
		ActorId:          g.NewVar(fieldsMap["actorId"]).Uint64(),
		Content:          fieldsMap["content"],
		NotificationType: fieldsMap["notificationType"],
		RelatedTaskId:    g.NewVar(fieldsMap["relatedTaskId"]).Uint64(),
		RetryCount:       g.NewVar(fieldsMap["retryCount"]).Uint64(),
	}
	// 7. 返回消息
	return &NotificationEventMessage{
		Id:      messageId,
		Payload: payload,
	}, nil
}

// AckNotificationEvent 确认一条 Stream 消息已经处理完成。
func AckNotificationEvent(ctx context.Context, messageId string) error {
	// 1. 校验消息 ID
	if messageId == "" {
		return gerror.New("messageId 不能为空")
	}
	// 2. 使用 XACK 确认消息
	value, err := g.Redis().Do(ctx, "XACK", notificationEventStreamKey, notificationEventGroupName, messageId)
	// 3. 判断是否成功确认
	if err != nil {
		return err
	}
	if value.Int() == 0 {
		return gerror.New("消息不存在或不属于当前消费组的pending列表")
	}
	// 4. 返回结果
	return nil
}

// ProcessOneNotificationEvent 读取并处理一条 Stream 通知事件。
func ProcessOneNotificationEvent(ctx context.Context, consumerName string) (bool, error) {
	// 1. 从 Stream 读取一条消息
	message, err := ReadOneNotificationEvent(ctx, consumerName)
	if err != nil {
		return false, err
	}
	// 2. 没有新消息时返回 false, nil
	if message == nil {
		return false, nil
	}
	// 3. 将通知写入 MySQL
	if err := processNotificationEventMessage(ctx, message); err != nil {
		return true, err
	}
	// 6. 返回 true, nil
	return true, nil
}

// GetNotificationEventPendingCount 查询消费组中等待确认的消息数量。
func GetNotificationEventPendingCount(ctx context.Context) (int, error) {
	// 1. 确保消费组存在
	if err := EnsureNotificationEventGroup(ctx); err != nil {
		return 0, err
	}
	// 2. 执行 XPENDING stream group
	value, err := g.Redis().Do(ctx, "XPENDING", notificationEventStreamKey, notificationEventGroupName)
	if err != nil {
		return 0, err
	}
	// 3. 解析 Pending 消息总数
	parts := value.Vars()
	if len(parts) == 0 {
		return 0, nil
	}
	// 4. 返回数量,	XPENDING 返回结果的第一个元素是整个消费组的 Pending 消息数量。
	return parts[0].Int(), nil

}

// ClaimOnePendingNotificationEvent 接管一条长时间未确认的消息。
func ClaimOnePendingNotificationEvent(
	ctx context.Context,
	consumerName string,
	minIdle time.Duration,
) (*NotificationEventMessage, error) {
	// 1. 校验消费者名称和最小闲置时间
	if consumerName == "" {
		return nil, gerror.New("consumerName 不能为空")
	}
	if minIdle <= 0 {
		return nil, gerror.New("minIdle 不能小于 0")
	}
	// 2. 确保消费组存在
	if err := EnsureNotificationEventGroup(ctx); err != nil {
		return nil, err
	}
	// 3. 执行 XAUTOCLAIM
	value, err := g.Redis().Do(ctx, "XAUTOCLAIM", notificationEventStreamKey, notificationEventGroupName, consumerName, minIdle.Milliseconds(), "0-0", "COUNT", 1) //这是什么意思？
	if err != nil {
		return nil, err
	}
	// 4. 没有可接管消息时返回 nil, nil
	if value.IsNil() {
		return nil, nil
	}
	// 5. 解析消息 ID 和字段
	parts := value.Vars()
	if len(parts) < 2 {
		return nil, gerror.New("XAUTOCLAIM 返回结果不完整")
	}
	messages := parts[1].Vars()
	if len(messages) == 0 {
		return nil, nil
	}
	messageParts := messages[0].Vars()
	if len(messageParts) < 2 {
		return nil, gerror.New("XAUTOCLAIM 返回结果不完整")
	}
	messageId := messageParts[0].String()
	fields := messageParts[1].Vars()
	fieldsMap := make(map[string]string)
	for i := 0; i+1 < len(fields); i += 2 {
		fieldsMap[fields[i].String()] = fields[i+1].String()
	}
	// 6. 返回被接管的消息
	return &NotificationEventMessage{
		Id: messageId,
		Payload: RetryNotificationPayload{
			ReceiverId:       g.NewVar(fieldsMap["receiverId"]).Uint64(),
			ActorId:          g.NewVar(fieldsMap["actorId"]).Uint64(),
			Content:          fieldsMap["content"],
			NotificationType: fieldsMap["notificationType"],
			RelatedTaskId:    g.NewVar(fieldsMap["relatedTaskId"]).Uint64(),
			RetryCount:       g.NewVar(fieldsMap["retryCount"]).Uint64(),
		},
	}, nil
}

// processNotificationEventMessage 处理一条已经读取到的 Stream 消息。
func processNotificationEventMessage(ctx context.Context, message *NotificationEventMessage) error {
	// 1. 校验 message
	if message == nil {
		return gerror.New("message 不能为空")
	}
	// 2. 从 message 中取 payload
	payload := message.Payload
	// 3. 写入 MySQL 通知记录
	notificationId, err := CreateNotificationRecord(ctx, nil, payload.ReceiverId, payload.ActorId, payload.NotificationType, payload.Content, payload.RelatedTaskId)
	if err != nil {
		return err
	}
	// 4. 写入 Redis 未读集合
	if err := AddUnreadNotificationToRedis(ctx, payload.ReceiverId, notificationId); err != nil {
		return err
	}
	// 5. XACK 确认消息
	if err := AckNotificationEvent(ctx, message.Id); err != nil {
		return err
	}
	// 6. 返回 nil
	return nil
}

// ProcessOneClaimedNotificationEvent 接管并处理一条超时 Pending 消息。
func ProcessOneClaimedNotificationEvent(
	ctx context.Context,
	consumerName string,
	minIdle time.Duration,
) (bool, error) {
	// 1. 调用 ClaimOnePendingNotificationEvent 接管消息
	message, err := ClaimOnePendingNotificationEvent(ctx, consumerName, minIdle)
	if err != nil {
		return false, err
	}
	// 2. 没有可接管消息时返回 false, nil
	if message == nil {
		return false, nil
	}
	// 3. 调用 processNotificationEventMessage 处理消息
	err = processNotificationEventMessage(ctx, message)
	if err != nil {
		return true, err
	}
	// 4. 返回 true, nil
	return true, nil
}

// StartNotificationEventWorker 启动 Stream 通知事件 worker。
func StartNotificationEventWorker(ctx context.Context) {
	// 1. 启动 goroutine
	go func() {
		ticker := time.NewTicker(notificationEventWorkerInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return

			case <-ticker.C:
				// 4. 每轮先处理新消息
				processed, err := ProcessOneNotificationEvent(ctx, notificationEventConsumerName)
				if err != nil {
					g.Log().Error(ctx, "notification event worker process new message failed", "err", err)
					continue //继续处理下一个事件
				}
				if processed {
					continue //继续处理下一个事件
				}

				// 5. 如果没有新消息，再尝试接管超时 Pending
				claimed, err := ProcessOneClaimedNotificationEvent(ctx, notificationEventConsumerName, notificationEventClaimMinIdle) //尝试在指定时间内没有被其他消费者处理的事件
				if err != nil {
					g.Log().Error(ctx, "notification event worker process pending message failed", "err", err)
					continue //继续处理下一个事件
				}

				if claimed {
					g.Log().Info(ctx, "notification event worker claimed pending message")
				}
			}
		}
	}()

}
