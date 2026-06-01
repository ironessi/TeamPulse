package task

import (
	"context"
	"database/sql"
	"errors"
	"redis-demo/internal/dao"
	"redis-demo/internal/model/entity"
	"strconv"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

const taskReminderKey = "task:reminder"

// SetReminder 给任务设置提醒时间。
func SetReminder(ctx context.Context, userId uint64, taskId uint64, remindAt int64) error {
	// 1. 查询任务是否存在
	var task entity.Task
	err := dao.Task.Ctx(ctx).Where("id", taskId).Scan(&task)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("任务不存在")
	}
	if err != nil {
		return err
	}
	// 2. 任务不存在时返回“任务不存在”
	if task.Id == 0 {
		return errors.New("任务不存在")
	}
	// 3. 已完成任务不允许设置提醒
	if task.Status == TaskStatusDone {
		return errors.New("已完成任务不允许设置提醒")
	}

	// 4. 校验当前用户是否属于任务所在团队
	count, err := dao.TeamMember.Ctx(ctx).Where("team_id", task.TeamId).Where("user_id", userId).Count()
	if err != nil {
		return err
	}

	// 5. 非团队成员返回“你没有权限设置该任务提醒”
	if count == 0 {
		return errors.New("你没有权限设置该任务提醒")
	}
	// 6. 校验 remindAt 必须晚于当前时间
	if remindAt <= time.Now().Unix() {
		return errors.New("提醒时间必须晚于当前时间")
	}
	// 7. 写入 Redis ZSet：score=remindAt，member=taskId
	_, err = g.Redis().Do(ctx, "ZADD", taskReminderKey, remindAt, strconv.FormatUint(taskId, 10)) // ZADD 命令返回被成功添加的新成员的数量（不包括那些已经存在的、仅更新分数的成员）。 --- IGNORE ---
	if err != nil {
		return err
	}
	// 8. 返回 nil
	return nil
}
