package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"redis-demo/internal/dao"
	lockLogic "redis-demo/internal/logic/lock"
	notificationLogic "redis-demo/internal/logic/notification"
	"redis-demo/internal/model/entity"
	"strconv"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

const (
	taskReminderKey            = "task:reminder"
	taskReminderScanInterval   = 5 * time.Second
	taskReminderScannerLockKey = "lock:task:reminder:scanner"
	taskReminderScannerLockTTL = 4 * time.Second
)

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

// 读取到期提醒任务的逻辑
func ScanDueReminders(ctx context.Context, now int64) error {
	// 1. 从 Redis ZSet 读取 score <= now 的 member。
	// task:reminder 的 score 是提醒时间戳，member 是 taskId；
	// 所以 ZRANGEBYSCORE task:reminder -inf now 表示找出所有已经到提醒时间的任务。
	values, err := g.Redis().Do(ctx, "ZRANGEBYSCORE", taskReminderKey, "-inf", now)
	if err != nil {
		return err
	}

	// 2. Redis 返回的是一组 member，这里先统一转成字符串切片。
	// 正常情况下每个 member 都是任务 ID 的字符串形式，例如 "2"、"14"。
	taskIds := values.Strings()

	// 3. 没有到期任务时直接返回 nil。
	if len(taskIds) == 0 {
		return nil
	}

	// 4. 逐个处理已经到期的任务 ID。
	for _, rawTaskId := range taskIds {
		// ZSet member 是字符串；查 MySQL 时需要 uint64 类型的任务 ID。
		taskId, err := strconv.ParseUint(rawTaskId, 10, 64)
		if err != nil {
			// 如果 Redis 中混入了 "abc" 这类脏 member，无法解析成 taskId。
			// 这里必须按原始 rawTaskId 删除，而不是按 taskId 删除，因为解析失败时 taskId 不可靠。
			if err := removeTaskReminderMember(ctx, rawTaskId); err != nil {
				return err
			}
			continue
		}

		// 5. 查询任务主数据。MySQL 仍然是真实数据源，Redis 这里只保存待提醒的 taskId。
		var task entity.Task
		err = dao.Task.Ctx(ctx).Where("id", taskId).Scan(&task)
		if errors.Is(err, sql.ErrNoRows) || task.Id == 0 {
			// 任务已经不存在，保留在 ZSet 中只会导致后续反复扫描，直接清理。
			if err := removeTaskReminder(ctx, taskId); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}

		// 6. 已完成任务不再提醒，直接清理 ZSet。
		if task.Status == TaskStatusDone {
			if err := removeTaskReminder(ctx, taskId); err != nil {
				return err
			}
			continue
		}

		// 7. 没有负责人时不知道通知谁，直接清理 ZSet。
		if task.AssigneeId == 0 {
			if err := removeTaskReminder(ctx, taskId); err != nil {
				return err
			}
			continue
		}

		content := fmt.Sprintf("任务「%s」已到提醒时间，请及时处理", task.Title)

		// 8. 给任务负责人创建提醒通知。actorId=0 表示系统触发。
		err = notificationLogic.CreateNotification(ctx, task.AssigneeId, 0, notificationLogic.TypeTaskReminder, content, task.Id)
		if err != nil {
			return err
		}

		// 9. 通知创建成功后再移除 ZSet 成员，避免先删除后通知失败导致提醒丢失。
		if err := removeTaskReminder(ctx, task.Id); err != nil {
			return err
		}
	}

	// 10. 全部到期提醒处理完成。
	return nil
}

func removeTaskReminder(ctx context.Context, taskId uint64) error {
	_, err := g.Redis().Do(ctx, "ZREM", taskReminderKey, strconv.FormatUint(taskId, 10))
	return err
}

func removeTaskReminderMember(ctx context.Context, member string) error {
	_, err := g.Redis().Do(ctx, "ZREM", taskReminderKey, member)
	return err
}

// StartReminderScanner 启动任务提醒扫描器。
// 多实例部署时，后续需要再加分布式锁，避免多个实例重复扫描同一批提醒。
func StartReminderScanner(ctx context.Context) {
	// 1. 启动 goroutine
	go func() {
		// 2. 创建 ticker
		ticker := time.NewTicker(taskReminderScanInterval) // 创建一个定时器，每隔 taskReminderScanInterval 触发一次。
		defer ticker.Stop()                                // 确保函数退出时停止 ticker，释放资源。

		for {
			// 3. 循环监听 ctx.Done 和 ticker.C
			select {
			case <-ctx.Done(): // ctx.Done() 代表外部调用取消了这个扫描器，应该退出。
				return

			case <-ticker.C: // ticker.C 代表定时器触发，应该执行一次任务提醒扫描。
				if err := scanDueRemindersWithLock(ctx, time.Now().Unix()); err != nil {
					g.Log().Error(ctx, "task reminder scanner error", "err", err)
				}
			}
		}
	}()
}

func scanDueRemindersWithLock(ctx context.Context, now int64) error {
	// 1. 尝试获取 scanner 分布式锁。
	lock, locked, err := lockLogic.TryLock(ctx, taskReminderScannerLockKey, taskReminderScannerLockTTL)
	if err != nil {
		return err
	}

	// 2. 没拿到锁说明其他实例正在扫描，跳过本轮，不算错误。
	if !locked {
		return nil
	}

	// 3. 拿到锁后确保最终释放，避免扫描过程新增分支时遗漏 Unlock。
	defer func() {
		if err := lockLogic.Unlock(ctx, lock); err != nil {
			g.Log().Error(ctx, "unlock task reminder scanner failed", "err", err)
		}
	}()

	// 4. 当前实例持有锁，执行本轮到期提醒扫描。
	return ScanDueReminders(ctx, now)
}

// CancelReminder 取消任务提醒。
func CancelReminder(ctx context.Context, userId uint64, taskId uint64) error {
	// 1. 查询任务是否存在
	var task entity.Task
	err := dao.Task.Ctx(ctx).Where("id", taskId).Scan(&task)
	// 2. 任务不存在时返回“任务不存在”
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("任务不存在")
	}
	if err != nil {
		return err
	}
	if task.Id == 0 {
		return errors.New("任务不存在")
	}
	// 3. 校验当前用户是否属于任务所在团队
	count, err := dao.TeamMember.Ctx(ctx).Where("team_id", task.TeamId).Where("user_id", userId).Count()
	if err != nil {
		return err
	}
	// 4. 非团队成员返回“你没有权限取消该任务提醒”
	if count == 0 {
		return errors.New("你没有权限取消该任务提醒")
	}

	// 5. 从 Redis ZSet 中移除 taskId
	err = removeTaskReminder(ctx, taskId)
	if err != nil {
		return err
	}
	// 6. 不管 ZREM 删除了 1 个还是 0 个，都返回 nil，保证幂等
	return nil
}
