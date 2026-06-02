package notification

import (
	"context"
	"fmt"
	v1 "redis-demo/api/notification/v1"
	"redis-demo/internal/dao"
	"redis-demo/internal/model/do"
	"redis-demo/internal/model/entity"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"
)

const (
	notificationListLimit = 20
	TypeTaskStatusUpdated = "task_status_updated"
	TypeTaskAssigned      = "task_assigned"
	TypeTaskCommented     = "task_commented"
	TypeTaskMentioned     = "task_mentioned"
	TypeTaskReminder      = "task_reminder"
)

// notificationUnreadKey 生成当前用户的未读通知集合 key。
func notificationUnreadKey(userId uint64) string {
	// 返回 notification:unread:{userId}
	return fmt.Sprintf("notification:unread:%d", userId)
}

// GetNotifications 查询当前用户最近的通知列表。
// userId 来自 JWT 鉴权上下文，查询条件限制为 receiver_id，
// 因此用户只能读取发送给自己的通知。
func GetNotifications(ctx context.Context, userId uint64) ([]v1.NotificationItem, error) {
	// 1. 从 MySQL 查询当前用户最近 20 条通知。
	// 通知内容以 MySQL 为真实数据源，Redis 只用于后续未读数量统计。
	var notifications []entity.Notification
	err := dao.Notification.Ctx(ctx).Where("receiver_id", userId).OrderDesc("created_at").Limit(notificationListLimit).Scan(&notifications)
	if err != nil {
		return nil, err
	}
	if len(notifications) == 0 {
		return []v1.NotificationItem{}, nil
	}

	// 2. 将数据库实体转换为 API 返回的数据结构。
	items := make([]v1.NotificationItem, 0, len(notifications))
	for _, notification := range notifications {
		items = append(items, v1.NotificationItem{
			NotificationId: notification.Id,
			ActorId:        notification.ActorId,
			Type:           notification.Type,
			Content:        notification.Content,
			RelatedTaskId:  notification.RelatedTaskId,
			IsRead:         notification.IsRead,
			CreatedAt:      notification.CreatedAt,
			ReadAt:         notification.ReadAt,
		})
	}

	// 3. 没有通知时返回空切片，前端会得到 [] 而不是 null。
	return items, nil

}

// GetUnreadCount 查询当前用户的未读通知数量。
// Redis Set 用于快速统计；当集合为空时，从 MySQL 未读记录重建缓存。
func GetUnreadCount(ctx context.Context, userId uint64) (uint64, error) {
	// 1. 生成当前用户的 Redis 未读集合 key
	key := notificationUnreadKey(userId)
	// 2. 从 Redis Set 查询当前未读数量
	count, err := g.Redis().SCard(ctx, key) //SCard() 返回集合的基数，即集合中元素的数量。
	if err != nil {
		return 0, err
	}
	// 3. 如果 Redis 中已有未读数据，直接返回数量
	if count > 0 {
		return uint64(count), nil
	}
	// 4. Redis 集合为空时，从 MySQL 查询当前用户未读通知 ID
	var unreadNotifications []entity.Notification
	err = dao.Notification.Ctx(ctx).Fields("id").Where("receiver_id", userId).Where("is_read", 0).Scan(&unreadNotifications)
	if err != nil {
		return 0, err
	}
	// 5. 如果 MySQL 也没有未读通知，返回 0
	if len(unreadNotifications) == 0 {
		return 0, nil
	}

	// 6. 将 MySQL 查询出的未读通知 ID 组装为 Redis 参数
	notificationIds := make([]any, 0, len(unreadNotifications))
	for _, notification := range unreadNotifications {
		notificationIds = append(notificationIds, notification.Id)
	}
	// 7. 将未读通知 ID 回填到 Redis Set
	_, err = g.Redis().SAdd(ctx, key, notificationIds[0], notificationIds[1:]...) //SAdd() 将一个或多个元素添加到集合中。
	if err != nil {
		return 0, err
	}
	// 8. 返回 MySQL 中的真实未读数量
	return uint64(len(unreadNotifications)), nil
}

// MarkAsRead 将当前用户的一条通知标记为已读。
func MarkAsRead(ctx context.Context, userId uint64, notificationId uint64) error {
	// 1. 根据 notificationId 查询通知记录
	var notification entity.Notification
	err := dao.Notification.Ctx(ctx).Where("id", notificationId).Scan(&notification)
	if err != nil {
		return err
	}

	// 2. 通知不存在时返回“通知不存在”
	if notification.Id == 0 {
		return gerror.New("通知不存在")
	}

	// 3. 校验该通知属于当前登录用户
	if notification.ReceiverId != userId {
		return gerror.New("你没有权限操作该通知")
	}
	// 4. 已读通知不重复更新 MySQL，但仍清理 Redis 未读集合。
	// 这样第一次请求若在 Redis 步骤失败，重试可以修复未读计数。
	if notification.IsRead == 1 {
		_, err := g.Redis().SRem(ctx, notificationUnreadKey(userId), notificationId)

		return err
	}

	// 5. 更新 MySQL 中的 is_read 和 read_at
	_, err = dao.Notification.Ctx(ctx).Where("id", notificationId).Data(g.Map{
		"is_read": 1,
		"read_at": gtime.Now(),
	}).Update()
	if err != nil {
		return err
	}

	// 6. 从 Redis 未读 Set 中移除 notificationId
	_, err = g.Redis().SRem(ctx, notificationUnreadKey(userId), notificationId) //SRem() 移除集合中的元素。
	if err != nil {
		return err
	}

	// 7. 返回 nil
	return nil
}

// CreateNotification 创建一条业务通知，并加入接收人的未读集合。
// 该函数供任务等业务 Logic 调用，不对前端直接开放。
func CreateNotification(
	ctx context.Context,
	receiverId uint64,
	actorId uint64,
	notificationType string,
	content string,
	relatedTaskId uint64,
) error {
	// 1. 如果接收人与触发人相同，直接返回，避免自己通知自己
	if receiverId == actorId {
		return nil
	}
	// 2. 向 MySQL 写入未读通知记录
	result, err := dao.Notification.Ctx(ctx).Data(do.Notification{
		ReceiverId:    receiverId,
		ActorId:       actorId,
		Type:          notificationType,
		Content:       content,
		RelatedTaskId: relatedTaskId,
		IsRead:        0, //默认未读
	}).Insert()
	if err != nil {
		return err
	}

	// 3. 获取新增通知 ID
	notificationId, err := result.LastInsertId()
	if err != nil {
		return err
	}
	// 4. 将通知 ID 加入 Redis 未读 Set
	_, err = g.Redis().SAdd(ctx, notificationUnreadKey(receiverId), notificationId) //SAdd() 将一个或多个元素添加到集合中。
	if err != nil {
		return err
	}
	// 5. 返回 nil
	return nil
}
