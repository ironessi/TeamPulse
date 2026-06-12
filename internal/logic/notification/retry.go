package notification

import (
	"context"
	"encoding/json"
	lockLogic "redis-demo/internal/logic/lock"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

const (
	notificationRetryQueueKey            = "notification:retry:queue"
	notificationRetryProcessingKey       = "notification:retry:processing"
	notificationRetryMaxCount            = 3
	notificationRetryWorkerInterval      = 5 * time.Second
	notificationRetryWorkerLockKey       = "lock:notification:retry:worker"
	notificationRetryWorkerLockTTL       = 4 * time.Second
	notificationRetryRecoverDefaultLimit = 100
	notificationRetryDeadKey             = "notification:retry:dead"
)

// RetryNotificationPayload 重试通知数据结构
type RetryNotificationPayload struct {
	// 1. 接收人 ID
	ReceiverId uint64 `json:"receiverId"`
	// 2. 触发人 ID
	ActorId uint64 `json:"actorId"`
	// 3. 通知类型
	NotificationType string `json:"notificationType"`
	// 4. 通知内容
	Content string `json:"content"`
	// 5. 关联任务 ID
	RelatedTaskId uint64 `json:"relatedTaskId"`
	// 6. 当前重试次数
	RetryCount uint64 `json:"retryCount"`
}

type notificationRetryMessage struct {
	Payload RetryNotificationPayload
	Raw     string
}

// EnqueueNotificationRetry 将失败通知写入 Redis 重试队列。
func EnqueueNotificationRetry(ctx context.Context, payload RetryNotificationPayload) error {
	// 1. 将 payload 转成 JSON
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	// 2. RPUSH 到 notification:retry:queue
	_, err = g.Redis().RPush(ctx, notificationRetryQueueKey, string(data))
	// 3. 返回错误或 nil
	if err != nil {
		return err
	}
	return nil
}

// DequeueNotificationRetry 从 Redis 重试队列取出一条通知。
// 使用 LMOVE 将消息从待处理队列移动到 processing 队列
// 避免 worker 取到消息后宕机导致消息直接丢失。
func DequeueNotificationRetry(ctx context.Context) (*RetryNotificationPayload, error) {
	message, err := dequeueNotificationRetryMessage(ctx)
	if err != nil {
		return nil, err
	}
	if message == nil {
		return nil, nil
	}
	return &message.Payload, nil
}

func dequeueNotificationRetryMessage(ctx context.Context) (*notificationRetryMessage, error) {
	// 1. LMOVE queue -> processing
	value, err := g.Redis().Do(
		ctx,
		"LMOVE",
		notificationRetryQueueKey,
		notificationRetryProcessingKey,
		"LEFT",
		"RIGHT",
	)
	if err != nil {
		return nil, err
	}
	// 2. 空队列返回 nil, nil
	if value.IsNil() {
		return nil, nil
	}
	// 3. 保存 raw 字符串
	raw := value.String()
	// 4. 反序列化成 payload
	var payload RetryNotificationPayload
	if err := json.Unmarshal([]byte(value.String()), &payload); err != nil {
		return nil, err
	}
	// 5. 返回 message
	return &notificationRetryMessage{
		Payload: payload,
		Raw:     raw,
	}, nil
}

func ackNotificationRetryMessage(ctx context.Context, raw string) error {
	// 1. raw 为空直接返回
	if raw == "" {
		return nil
	}
	// 2. LREM processing 1 raw
	_, err := g.Redis().LRem(ctx, notificationRetryProcessingKey, 1, raw)
	// 3. 返回 error
	return err
}

// ProcessOneNotificationRetry 处理一条通知重试任务。
func ProcessOneNotificationRetry(ctx context.Context) (bool, error) {
	// 1. 从 Redis 队列取出一条 payload
	message, err := dequeueNotificationRetryMessage(ctx)
	if err != nil {
		return false, err
	}
	// 2. 队列为空时返回 false, nil
	if message == nil {
		return false, nil
	}
	payload := message.Payload
	// 3. 重试 worker 直接创建 MySQL 通知记录，避免调用 CreateNotification 时失败路径重复入队。
	notificationId, err := CreateNotificationRecord(ctx, nil, payload.ReceiverId, payload.ActorId, payload.NotificationType, payload.Content, payload.RelatedTaskId)

	// 4. 创建成功，返回 true, nil
	if err == nil {
		if ackErr := ackNotificationRetryMessage(ctx, message.Raw); ackErr != nil {
			return false, ackErr
		}
		if redisErr := AddUnreadNotificationToRedis(ctx, payload.ReceiverId, notificationId); redisErr != nil {
			return true, redisErr
		}
		return true, nil
	}

	// 5. 创建失败时，先从 processing 删除旧消息。
	// 后续如果还需要重试，会把 retryCount+1 的新 payload 重新放回 queue。
	if ackErr := ackNotificationRetryMessage(ctx, message.Raw); ackErr != nil {
		return true, ackErr
	}

	// 6. 创建失败时增加 RetryCount
	payload.RetryCount++

	// 7. RetryCount 未超过最大次数时重新入队
	if payload.RetryCount <= notificationRetryMaxCount {
		if enqueueErr := EnqueueNotificationRetry(ctx, payload); enqueueErr != nil {
			return true, enqueueErr
		}
	} else {
		if deadErr := EnqueueDeadNotificationRetry(ctx, payload); deadErr != nil {
			return true, deadErr
		}
	}

	// 8. 返回 true, err
	return true, err
}

// StartNotificationRetryWorker 启动通知重试后台 worker。
func StartNotificationRetryWorker(ctx context.Context) {
	// 1. 启动 goroutine
	go func() {
		// 2. 创建 ticker，每隔 notificationRetryWorkerInterval 触发一次
		ticker := time.NewTicker(notificationRetryWorkerInterval)
		// 3. defer ticker.Stop()
		defer ticker.Stop()
		// 4. for + select 监听 ctx.Done() 和 ticker.C
		for {
			select {
			// 5. ctx.Done() 触发时退出 goroutine
			case <-ctx.Done():
				return

			case <-ticker.C:
				// 6. 调用 ProcessOneNotificationRetry
				processed, err := processOneNotificationRetryWithLock(ctx)
				if err != nil {
					g.Log().Error(ctx, "notification retry worker error", "processed", processed, "err", err)
				}
			}
		}
	}()
}

// processOneNotificationRetryWithLock 带分布式锁处理一条通知重试任务。
func processOneNotificationRetryWithLock(ctx context.Context) (bool, error) {
	// 1. 尝试获取 worker 分布式锁
	lock, locked, err := lockLogic.TryLock(ctx, notificationRetryWorkerLockKey, notificationRetryWorkerLockTTL)
	if err != nil {
		return false, err
	}
	// 2. 拿不到锁时返回 false, nil，表示本轮跳过
	if !locked {
		return false, nil
	}

	// 3. 拿到锁后 defer Unlock
	defer func() {
		if err := lockLogic.Unlock(ctx, lock); err != nil {
			g.Log().Error(ctx, "unlock notification retry worker failed", "err", err)
		}
	}()
	return ProcessOneNotificationRetry(ctx)
}

// RecoverProcessingNotificationRetries 将 processing 队列中的消息恢复回 retry queue。
// 这个函数用于处理 worker 宕机后遗留在 processing 中的消息。
// 第一版不判断超时，只做手动恢复：processing -> queue。
func RecoverProcessingNotificationRetries(ctx context.Context, limit int) (int, error) {
	// 1. limit <= 0 时使用默认值
	if limit <= 0 {
		limit = notificationRetryRecoverDefaultLimit
	}
	// 2. 循环最多恢复 limit 条
	recovered := 0
	for recovered < limit {
		value, err := g.Redis().Do(ctx, "LMOVE", notificationRetryProcessingKey, notificationRetryQueueKey, "LEFT", "RIGHT")
		if err != nil {
			return recovered, err
		}
		if value.IsNil() {
			return recovered, nil
		}

		recovered++
	}
	return recovered, nil
}

// EnqueueDeadNotificationRetry 将超过最大重试次数的通知写入死信队列。
func EnqueueDeadNotificationRetry(ctx context.Context, payload RetryNotificationPayload) error {
	// 1. payload 转 JSON
	value, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	// 2. RPUSH 到 notification:retry:dead
	_, err = g.Redis().RPush(ctx, notificationRetryDeadKey, string(value))
	// 3. 返回 error
	return err
}
