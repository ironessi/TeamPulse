package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	taskV1 "redis-demo/api/task/v1"
	teamV1 "redis-demo/api/team/v1"
	"redis-demo/internal/dao"
	notificationLogic "redis-demo/internal/logic/notification"
	"redis-demo/internal/logic/team"
	"redis-demo/internal/model/do"
	"redis-demo/internal/model/entity"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
)

type pendingUnreadNotification struct {
	ReceiverId     uint64
	NotificationId uint64
}

// CreateComment 创建任务评论。
func CreateComment(ctx context.Context, userId uint64, taskId uint64, content string, mentionUserIds []uint64) (uint64, error) {
	// 1. 去掉评论内容前后空格,避免用户输入全空格的评论
	content = strings.TrimSpace(content) //作用：去掉 content 字符串前后的空格，避免用户输入全空格的评论被误认为有效内容。
	// 2. 评论内容为空时返回错误
	if content == "" { //如果 content 经过 TrimSpace 后是空字符串，说明用户输入的评论内容全是空格，这时应该返回错误提示用户输入有效的评论内容。为什么不判断len(content) == 0？因为用户可能输入了全空格的评论，len(content) 可能大于 0，但实际内容是无效的，所以需要先 TrimSpace 去掉空格后再判断是否为空字符串。
		return 0, gerror.New("请输入评论内容")
	}

	// 3. 根据 taskId 查询任务
	var task entity.Task
	err := dao.Task.Ctx(ctx).Where("id", taskId).Scan(&task)
	if errors.Is(err, sql.ErrNoRows) { // 这是什么意思？当根据 taskId 查询任务时，如果没有找到对应的任务，数据库会返回 sql.ErrNoRows 错误。使用 errors.Is(err, sql.ErrNoRows) 来判断是否是因为没有找到任务而导致的错误。如果是这种情况，说明任务不存在，我们应该返回一个友好的错误提示用户“任务不存在”。如果 err 不是 sql.ErrNoRows，那么可能是其他数据库错误，我们应该直接返回这个错误给调用者。
		return 0, gerror.New("任务不存在")
	}
	if err != nil {
		return 0, err
	}
	// 4. 任务不存在时返回“任务不存在”
	if task.Id == 0 {
		return 0, gerror.New("任务不存在")
	}

	// 5. 校验当前用户是否属于任务所在团队
	count, err := dao.TeamMember.Ctx(ctx).Where("team_id", task.TeamId).Where("user_id", userId).Count()
	if err != nil {
		return 0, err
	}
	// 6. 非团队成员返回“你没有权限评论该任务”
	if count == 0 {
		return 0, gerror.New("你没有权限评论该任务")
	}
	// 7. 校验 mentionUserIds 中的用户是否属于同一团队
	for _, mentionUserId := range mentionUserIds {
		if mentionUserId == 0 {
			return 0, gerror.New("提及用户不能为空")
		}

		count, err = dao.TeamMember.Ctx(ctx).
			Where("team_id", task.TeamId).
			Where("user_id", mentionUserId).
			Count()
		if err != nil {
			return 0, err
		}
		if count == 0 {
			return 0, gerror.New("提及用户不是该团队成员")
		}
	}

	var commentId uint64
	pendingUnreadNotifications := make([]pendingUnreadNotification, 0) //创建一个切片 pendingUnreadNotifications，用于存储待处理的未读通知。
	notifiedUserIds := make(map[uint64]struct{})                       // 记录本次评论已经通知过的用户 ID，避免负责人被 mention 后再收到重复通知。
	err = dao.TaskComment.Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		// 8. 写入 MySQL task_comment
		result, err := tx.Model(dao.TaskComment.Table()).Ctx(ctx).Data(do.TaskComment{
			TaskId:  taskId,
			TeamId:  task.TeamId,
			UserId:  userId,
			Content: content,
		}).Insert()
		if err != nil {
			return err
		}

		insertId, err := result.LastInsertId()
		if err != nil {
			return err
		}

		commentId = uint64(insertId)

		// 处理被提及成员的 task_mentioned 通知。
		for _, mentionUserId := range mentionUserIds {
			if mentionUserId == userId {
				continue
			}
			notificationId, err := notificationLogic.CreateNotificationRecord(ctx, tx, mentionUserId, userId, notificationLogic.TypeTaskMentioned, fmt.Sprintf("用户%d在任务%s的评论中提到了你", userId, task.Title), taskId)
			if err != nil {
				return err
			}
			if notificationId > 0 {
				pendingUnreadNotifications = append(pendingUnreadNotifications, pendingUnreadNotification{
					ReceiverId:     mentionUserId,
					NotificationId: notificationId,
				})
				notifiedUserIds[mentionUserId] = struct{}{}
			}
		}

		// 负责人通知也只写 MySQL notification 记录。
		if task.AssigneeId > 0 && task.AssigneeId != userId {
			// 如果负责人已经因为 mention 收到通知，不重复创建 task_commented。
			if _, alreadyNotified := notifiedUserIds[task.AssigneeId]; !alreadyNotified {
				// 1. CreateNotificationRecord(ctx, tx, ...)
				notificationId, err := notificationLogic.CreateNotificationRecord(ctx, tx, task.AssigneeId, userId, notificationLogic.TypeTaskCommented, fmt.Sprintf("用户%d评论了你负责的任务%s", userId, task.Title),
					task.Id)
				if err != nil {
					return err
				}
				// 2. notificationId > 0 时追加到 pendingUnreadNotifications
				if notificationId > 0 {
					pendingUnreadNotifications = append(pendingUnreadNotifications, pendingUnreadNotification{
						ReceiverId:     task.AssigneeId,
						NotificationId: notificationId,
					})
				}
			}
		}

		return nil
	})
	if err != nil {
		return 0, err
	}

	// 10. 写入团队动态流 team:activities:{teamId}
	if err := team.AddActivity(ctx, task.TeamId, teamV1.ActivityItem{
		Action:    "task_commented",
		ActorId:   userId,
		Content:   fmt.Sprintf("用户%d评论了任务：%s", userId, task.Title),
		CreatedAt: time.Now().Unix(),
	}); err != nil {
		return 0, err
	}

	for _, item := range pendingUnreadNotifications {
		if err := notificationLogic.AddUnreadNotificationToRedis(ctx, item.ReceiverId, item.NotificationId); err != nil {
			return 0, err
		}
	}

	// 11. 返回 commentId
	return uint64(commentId), nil
}

func ListComments(ctx context.Context, userId uint64, taskId uint64) ([]taskV1.CommentItem, error) {
	// 1. 查询任务是否存在
	var task entity.Task
	err := dao.Task.Ctx(ctx).Where("id", taskId).Scan(&task)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, gerror.New("任务不存在")
	}
	if err != nil {
		return nil, err
	}
	// 2. 任务不存在时返回“任务不存在”
	if task.Id == 0 {
		return nil, gerror.New("任务不存在")
	}
	// 3. 校验当前用户是否属于任务所在团队
	count, err := dao.TeamMember.Ctx(ctx).Where("team_id", task.TeamId).Where("user_id", userId).Count()
	if err != nil {
		return nil, err
	}
	// 4. 非团队成员返回“你没有权限查看该任务评论”
	if count == 0 {
		return nil, gerror.New("你没有权限查看该任务评论")
	}
	// 5. 查询 task_comment，按 id 升序
	var taskComments []entity.TaskComment
	err = dao.TaskComment.Ctx(ctx).Where("task_id", taskId).OrderAsc("id").Scan(&taskComments)
	if err != nil {
		return nil, err
	}
	// 6. 把 entity.TaskComment 转成 taskV1.CommentItem
	commentItems := make([]taskV1.CommentItem, 0, len(taskComments))
	for _, taskComment := range taskComments {
		var createdAt int64
		if taskComment.CreatedAt != nil {
			createdAt = taskComment.CreatedAt.Unix() //如果 taskComment.CreatedAt 不为 nil，则将其转换为 Unix 时间戳（秒）并赋值给 createdAt 变量。这样做是为了确保在返回给前端的 CommentItem 中，CreatedAt 字段是一个整数类型的 Unix 时间戳，方便前端进行时间显示和处理。如果 taskComment.CreatedAt 为 nil，则 createdAt 将保持默认值 0，表示没有创建时间。
		}
		commentItems = append(commentItems, taskV1.CommentItem{
			CommentId: taskComment.Id,
			TaskId:    taskComment.TaskId,
			TeamId:    taskComment.TeamId,
			UserId:    taskComment.UserId,
			Content:   taskComment.Content,
			CreatedAt: createdAt,
		})
	}
	// 7. 返回评论列表
	return commentItems, nil
}
