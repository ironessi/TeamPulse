package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	mathRand "math/rand"
	taskV1 "redis-demo/api/task/v1"
	teamV1 "redis-demo/api/team/v1"
	"redis-demo/internal/dao"
	lockLogic "redis-demo/internal/logic/lock"
	notificationLogic "redis-demo/internal/logic/notification"
	"redis-demo/internal/logic/team"
	"redis-demo/internal/model/do"
	"redis-demo/internal/model/entity"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
)

const (
	TaskStatusTodo           = "todo"
	TaskStatusDoing          = "doing"
	TaskStatusDone           = "done"
	taskDetailCacheNullValue = "__NULL__"
	taskDetailCacheExpire    = 5 * time.Minute
	taskDetailCacheJitterMax = 60 * time.Second
	taskDetailNullExpire     = 60 * time.Second
	taskDetailLockExpire     = 10 * time.Second
	taskDetailWaitRetry      = 50 * time.Millisecond
	taskHotDailyExpire       = 7 * 24 * time.Hour
	taskHotWeeklyExpire      = 30 * 24 * time.Hour
)

// taskHotKey 生成团队热门任务排行榜 key。
// 例如 teamId=7 时，key 为：team:task:hot:7
func taskHotKey(teamId uint64) string {
	return fmt.Sprintf("team:task:hot:%d", teamId)
}

func taskDetailCacheKey(taskId uint64) string {
	// 返回 task:detail:{taskId}
	return fmt.Sprintf("task:detail:%d", taskId)
}

// CreateTask 创建团队任务。
// 当前用户必须属于该团队；指定负责人时，负责人也必须属于该团队。
func CreateTask(ctx context.Context, creatorId uint64, teamId uint64, title string, description string, assigneeId uint64, priority uint) (uint64, error) {
	// 1. 校验当前用户是否属于团队，团队成员才可以创建任务。
	count, err := dao.TeamMember.Ctx(ctx).Where("team_id", teamId).Where("user_id", creatorId).Count()
	if err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, gerror.New("你不是该团队成员")
	}

	// 2. 未分配负责人时写入 NULL；有负责人时校验其属于该团队。
	var assigneeValue any
	if assigneeId > 0 {
		count, err = dao.TeamMember.Ctx(ctx).Where("team_id", teamId).Where("user_id", assigneeId).Count()
		if err != nil {
			return 0, err
		}
		if count == 0 {
			return 0, gerror.New("负责人不是该团队成员")
		}
		assigneeValue = assigneeId
	}

	// 3. 插入任务记录，初始状态统一为 todo。
	result, err := dao.Task.Ctx(ctx).Data(do.Task{
		TeamId:      teamId,
		Title:       title,
		Description: description,
		CreatorId:   creatorId,
		AssigneeId:  assigneeValue,
		Priority:    priority,
		Status:      TaskStatusTodo,
	}).Insert()
	if err != nil {
		return 0, err
	}

	taskId, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	// 4. 创建任务后写入团队动态流，继续复用 Redis List。
	if err := team.AddActivity(ctx, teamId, teamV1.ActivityItem{
		Action:    "task_created",
		ActorId:   creatorId,
		Content:   fmt.Sprintf("用户%d创建了任务：%s", creatorId, title),
		CreatedAt: time.Now().Unix(),
	}); err != nil {
		return 0, err
	}

	return uint64(taskId), nil
}

func GetTasks(ctx context.Context, userId uint64, teamId uint64) ([]taskV1.TaskItem, error) {
	// 1. 校验当前用户是否属于团队
	count, err := dao.TeamMember.Ctx(ctx).Where("team_id", teamId).Where("user_id", userId).Count()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, gerror.New("你没有权限查看该团队的任务")
	}
	// 2. 查询这个团队下的任务记录
	var tasks []entity.Task
	err = dao.Task.Ctx(ctx).Where("team_id", teamId).OrderDesc("id").Scan(&tasks)
	if err != nil {
		return nil, err
	}
	// 3. 把 entity.Task 转换成 taskV1.TaskItem
	items := make([]taskV1.TaskItem, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, taskV1.TaskItem{
			TaskId:      task.Id,
			CreatorId:   task.CreatorId,
			Title:       task.Title,
			Description: task.Description,
			AssigneeId:  task.AssigneeId,
			Priority:    task.Priority,
			Status:      task.Status,
		})
	}
	// 4. 返回结果
	return items, nil

}

func GetTask(ctx context.Context, userId uint64, taskId uint64) (*taskV1.TaskItem, error) {
	// 1. 先查 Redis 任务详情缓存。
	// hit=false 表示 Redis 没有这个 key；hit=true 表示已经从缓存得到结果。
	// 如果命中 "__NULL__"，getTaskFromCache 会直接返回“任务不存在”，用于防缓存穿透。
	task, hit, err := getTaskFromCache(ctx, taskId)
	if err != nil {
		return nil, err
	}

	// 2. 缓存未命中时，进入“缓存击穿”处理。
	// 热门 key 过期的一瞬间，可能有大量请求同时进来；
	// 这里用 Redis 分布式锁控制只有一个请求去查 MySQL 并重建缓存。
	if !hit {
		lock, locked, err := lockLogic.TryLock(ctx, taskDetailLockKey(taskId), taskDetailLockExpire)
		if err != nil {
			return nil, err
		}

		if locked {
			// 3. 拿到锁的请求负责回源 MySQL，并把查询结果重新写回 Redis。
			// defer 保证函数返回前释放锁，避免后续请求一直等锁过期。
			defer func() {
				_ = lockLogic.Unlock(ctx, lock)
			}()

			task, err = getTaskFromDBAndCache(ctx, taskId)
			if err != nil {
				return nil, err
			}
		} else {
			// 4. 没拿到锁，说明可能已经有另一个请求正在重建缓存。
			// 先短暂等待，再读一次 Redis，优先复用别人刚刚写好的缓存结果。
			time.Sleep(taskDetailWaitRetry) // 等待 50 毫秒，避免频繁重试导致雪崩。

			task, hit, err = getTaskFromCache(ctx, taskId)
			if err != nil {
				return nil, err
			}

			if !hit {
				// 5. 等待后仍然没有缓存，说明持锁请求可能失败或还没写完。
				// 当前版本选择降级查库，保证接口可用；后续可改为继续重试或返回“稍后再试”。
				task, err = getTaskFromDBAndCache(ctx, taskId)
				if err != nil {
					return nil, err
				}
			}
		}
	}

	// 6. 权限校验必须放在返回数据之前。
	// 注意：即使任务来自 Redis 缓存，也不能直接返回；
	// 因为缓存 key 只按 taskId 区分，不按用户区分，必须确认当前用户属于任务所在团队。
	count, err := dao.TeamMember.Ctx(ctx).Where("team_id", task.TeamId).Where("user_id", userId).Count()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, gerror.New("你没有权限查看该任务")
	}

	// 7. 记录任务访问热度，用于热门任务排行榜。
	if err := increaseTaskHeat(ctx, task.TeamId, task.Id, 1); err != nil {
		return nil, err
	}

	// 8. 把数据库实体转换成 API 返回结构，避免直接把 entity 暴露给接口层。
	return &taskV1.TaskItem{
		TaskId:      task.Id,
		CreatorId:   task.CreatorId,
		Title:       task.Title,
		Description: task.Description,
		AssigneeId:  task.AssigneeId,
		Priority:    task.Priority,
		Status:      task.Status,
	}, nil
}

// UpdateTask 更新任务的可编辑信息；任务状态由 UpdateStatus 单独修改。
func UpdateTask(ctx context.Context, operatorId uint64, taskId uint64, title string, description string, assigneeId uint64, priority uint) error {
	var task entity.Task
	err := dao.Task.Ctx(ctx).Where("id", taskId).Scan(&task)
	if errors.Is(err, sql.ErrNoRows) {
		return gerror.New("任务不存在")
	}
	if err != nil {
		return err
	}
	if task.Id == 0 {
		return gerror.New("任务不存在")
	}

	count, err := dao.TeamMember.Ctx(ctx).Where("team_id", task.TeamId).Where("user_id", operatorId).Count()
	if err != nil {
		return err
	}
	if count == 0 {
		return gerror.New("你没有权限修改该任务")
	}

	var assigneeValue any
	if assigneeId > 0 {
		count, err = dao.TeamMember.Ctx(ctx).Where("team_id", task.TeamId).Where("user_id", assigneeId).Count()
		if err != nil {
			return err
		}
		if count == 0 {
			return gerror.New("负责人不是该团队成员")
		}
		assigneeValue = assigneeId
	}

	if task.Title == title &&
		task.Description == description &&
		task.AssigneeId == assigneeId &&
		task.Priority == int(priority) {
		return nil
	}

	_, err = dao.Task.Ctx(ctx).Where("id", taskId).Data(g.Map{
		"title":       title,
		"description": description,
		"assignee_id": assigneeValue,
		"priority":    priority,
	}).Update()
	if err != nil {
		return err
	}

	// 编辑任务后删除旧的任务详情缓存，让下一次读取时从 MySQL 重建。
	if err := deleteTaskDetailCache(ctx, taskId); err != nil {
		return err
	}

	if err := team.AddActivity(ctx, task.TeamId, teamV1.ActivityItem{
		Action:    "task_updated",
		ActorId:   operatorId,
		Content:   fmt.Sprintf("用户%d更新了任务：%s", operatorId, title),
		CreatedAt: time.Now().Unix(),
	}); err != nil {
		return err
	}

	if err = increaseTaskHeat(ctx, task.TeamId, task.Id, 1); err != nil {
		return err
	}

	// 2. 判断负责人是否发生变化，且新负责人非零
	if assigneeId > 0 && assigneeId != task.AssigneeId {
		if err := notificationLogic.CreateNotification(ctx, assigneeId, operatorId, notificationLogic.TypeTaskAssigned, fmt.Sprintf("用户%d将任务%s交给了您", operatorId, title), task.Id); err != nil {
			return err
		}
	}

	// 4. 返回 nil
	return nil
}

func UpdateStatus(ctx context.Context, operatorId uint64, taskId uint64, status string) error {
	// 1. 查询任务是否存在
	var task entity.Task
	err := dao.Task.Ctx(ctx).Where("id", taskId).Scan(&task)
	if errors.Is(err, sql.ErrNoRows) {
		return gerror.New("任务不存在")
	}
	if err != nil {
		return err
	}
	if task.Id == 0 {
		return gerror.New("任务不存在")
	}
	// 2. 校验操作者是否属于任务所在团队
	count, err := dao.TeamMember.Ctx(ctx).Where("team_id", task.TeamId).Where("user_id", operatorId).Count()
	if err != nil {
		return err
	}
	if count == 0 {
		return gerror.New("你没有权限修改该任务")
	}

	// 3. 如果状态没变化，直接返回
	if task.Status == status {
		return nil
	}

	// 4. 更新任务状态
	_, err = dao.Task.Ctx(ctx).Where("id", taskId).Data(do.Task{
		Status: status,
	}).Update()
	if err != nil {
		return err
	}

	// 5. 删除旧任务详情缓存
	if err := deleteTaskDetailCache(ctx, taskId); err != nil {
		return err
	}

	// 6. 写入团队动态
	if err := team.AddActivity(ctx, task.TeamId, teamV1.ActivityItem{
		Action:    "task_status_updated",
		ActorId:   operatorId,
		Content:   fmt.Sprintf("用户%d将任务%s的状态更新为%s", operatorId, task.Title, status),
		CreatedAt: time.Now().Unix(),
	}); err != nil {
		return err
	}

	// 7. 更新任务热度
	if err = increaseTaskHeat(ctx, task.TeamId, task.Id, 1); err != nil {
		return err
	}
	// 8. 如果任务有负责人，且负责人不是操作者本人，则创建状态变化通知
	if task.AssigneeId > 0 && task.AssigneeId != operatorId {
		if err := notificationLogic.CreateNotification(ctx, task.AssigneeId, operatorId, notificationLogic.TypeTaskStatusUpdated, fmt.Sprintf("用户%d将任务%s的状态更新为%s", operatorId, task.Title, status), task.Id); err != nil {
			return err
		}
	}
	// 9. 返回 nil
	return nil
}

func GetHotTasks(ctx context.Context, userId uint64, teamId uint64) ([]taskV1.HotTaskItem, error) {
	return getHotTasksByKey(ctx, userId, teamId, taskHotKey(teamId))
}

// deleteTaskDetailCache 删除任务详情缓存。
// 任务被编辑或状态变化后，删除缓存，让下一次读取从 MySQL 重建。
func deleteTaskDetailCache(ctx context.Context, taskId uint64) error {
	// 1. 生成 task:detail:{taskId}
	key := taskDetailCacheKey(taskId)
	// 2. 删除 Redis 缓存
	_, err := g.Redis().Del(ctx, key)
	// 3. 返回错误
	return err
}

// taskDetailCacheTTL 生成任务详情缓存的 TTL。
func taskDetailCacheTTL() time.Duration {
	// 1. 生成 0 到 taskDetailCacheJitterMax 之间的随机秒数
	jitterSeconds := mathRand.Int63n(int64(taskDetailCacheJitterMax.Seconds()) + 1)
	// 2. 返回 taskDetailCacheExpire + 随机抖动
	return taskDetailCacheExpire + time.Duration(jitterSeconds)*time.Second
}

// taskDetailLockKey 生成任务详情锁的 Redis key。
func taskDetailLockKey(taskId uint64) string {
	// 返回 lock:task:detail:{taskId}
	return fmt.Sprintf("lock:task:detail:%d", taskId)
}

// 从 Redis 读取任务详情缓存的函数 getTaskFromCache 和从 MySQL 读取并重建缓存的函数 getTaskFromDBAndCache 都放在 task.go 中，因为它们都是 GetTask 这个核心功能的组成部分，且它们之间有直接的调用关系。把它们放在一起可以更清晰地看到 GetTask 的整体实现逻辑，以及缓存和数据库之间的交互细节。同时，这些函数虽然涉及 Redis 和 MySQL，但它们的职责都是围绕任务详情的获取和缓存管理，因此放在 task.go 中也符合单一职责原则。
func getTaskFromCache(ctx context.Context, taskId uint64) (*entity.Task, bool, error) {
	key := taskDetailCacheKey(taskId)

	cacheValue, err := g.Redis().Get(ctx, key)
	if err != nil {
		return nil, false, err
	}
	if cacheValue.IsNil() {
		// Redis 中没有 task:detail:{taskId}，说明缓存未命中，需要继续查 MySQL。
		return nil, false, nil
	}

	value := cacheValue.String()
	if value == taskDetailCacheNullValue {
		// "__NULL__" 是空值缓存：表示 MySQL 已确认该任务不存在。
		// 这样大量请求查询同一个不存在 taskId 时，不会反复打到 MySQL。
		return nil, true, gerror.New("任务不存在")
	}
	var task entity.Task
	if err := json.Unmarshal([]byte(value), &task); err != nil {
		return nil, false, err
	}
	return &task, true, nil
}
func getTaskFromDBAndCache(ctx context.Context, taskId uint64) (*entity.Task, error) {
	// 1. 查询 MySQL
	var task entity.Task
	err := dao.Task.Ctx(ctx).Where("id", taskId).Scan(&task)
	if errors.Is(err, sql.ErrNoRows) {
		// 2. MySQL 不存在，写 "__NULL__" 短 TTL。
		// 这是缓存穿透保护：不存在的数据也缓存一小会儿，挡住重复无效查询。
		if err := g.Redis().SetEX(ctx, taskDetailCacheKey(taskId), taskDetailCacheNullValue, int64(taskDetailNullExpire.Seconds())); err != nil {
			return nil, err
		}
		return nil, gerror.New("任务不存在")
	}
	if err != nil {
		return nil, err
	}
	if task.Id == 0 {
		// GoFrame Scan 在某些情况下不会返回 sql.ErrNoRows，而是留下零值结构体。
		// 所以这里再用 task.Id == 0 兜底判断一次“任务不存在”。
		if err := g.Redis().SetEX(ctx, taskDetailCacheKey(taskId), taskDetailCacheNullValue, int64(taskDetailNullExpire.Seconds())); err != nil {
			return nil, err
		}
		return nil, gerror.New("任务不存在")
	}

	// 3. MySQL 存在，写任务 JSON，TTL 带随机抖动
	bytes, err := json.Marshal(task)
	if err != nil {
		return nil, err
	}
	ttl := taskDetailCacheTTL()
	// TTL 加随机抖动是为了防缓存雪崩：避免大量任务详情 key 在同一秒同时过期。
	if err := g.Redis().SetEX(ctx, taskDetailCacheKey(taskId), string(bytes), int64(ttl.Seconds())); err != nil {
		return nil, err
	}

	// 4. 返回 task
	return &task, nil
}

// taskHotDailyKey 生成团队热门任务日榜 key。
func taskHotDailyKey(teamId uint64, now time.Time) string {
	// 返回 team:task:hot:daily:{teamId}:{yyyyMMdd}
	return fmt.Sprintf("team:task:hot:daily:%d:%s", teamId, now.Format("20060102"))
}

// increaseTaskHeat 增加任务热度。
func increaseTaskHeat(ctx context.Context, teamId uint64, taskId uint64, score float64) error {
	// 1. 写入总榜 team:task:hot:{teamId}
	_, err := g.Redis().ZIncrBy(ctx, taskHotKey(teamId), score, taskId)
	if err != nil {
		return err
	}
	// 2. 生成今天的日榜 key
	dailyKey := taskHotDailyKey(teamId, time.Now())
	// 3. 写入日榜
	_, err = g.Redis().ZIncrBy(ctx, dailyKey, score, taskId)
	if err != nil {
		return err
	}
	// 4. 给日榜设置过期时间
	_, err = g.Redis().Expire(ctx, dailyKey, int64(taskHotDailyExpire.Seconds()))
	if err != nil {
		return err
	}

	// 5. 获取周榜 key
	weeklyKey := taskHotWeeklyKey(teamId, time.Now())
	// 6. 写入周榜
	_, err = g.Redis().ZIncrBy(ctx, weeklyKey, score, taskId)
	if err != nil {
		return err
	}
	// 7. 给周榜设置过期时间
	_, err = g.Redis().Expire(ctx, weeklyKey, int64(taskHotWeeklyExpire.Seconds()))
	if err != nil {
		return err
	}

	// 5. 返回 nil
	return nil
}

func GetDailyHotTasks(ctx context.Context, userId uint64, teamId uint64, now time.Time) ([]taskV1.HotTaskItem, error) {
	// 1. 生成日榜 key
	key := taskHotDailyKey(teamId, now)
	// 2. 复用 getHotTasksByKey 查询排行榜
	return getHotTasksByKey(ctx, userId, teamId, key)
}

func GetWeeklyHotTasks(ctx context.Context, userId uint64, teamId uint64, now time.Time) ([]taskV1.HotTaskItem, error) {
	key := taskHotWeeklyKey(teamId, now)
	return getHotTasksByKey(ctx, userId, teamId, key)
}

func getHotTasksByKey(ctx context.Context, userId uint64, teamId uint64, hotKey string) ([]taskV1.HotTaskItem, error) {
	// 1. 校验当前用户属于团队
	count, err := dao.TeamMember.Ctx(ctx).Where("team_id", teamId).Where("user_id", userId).Count()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, gerror.New("你没有权限查看该团队的热门任务")
	}
	// 2. 从 Redis 指定 key 读取前 10 个 taskId
	values, err := g.Redis().ZRevRange(ctx, hotKey, 0, 9) // 从 Redis 获取数据
	if err != nil {
		return nil, err
	}
	taskIds := values.Uints()
	// 3. 没有数据返回空切片
	if len(taskIds) == 0 {
		return []taskV1.HotTaskItem{}, nil
	}
	items := make([]taskV1.HotTaskItem, 0, len(taskIds))
	// 4. 逐个查询 MySQL 任务
	for _, taskId := range taskIds {
		var task entity.Task
		err := dao.Task.Ctx(ctx).Where("id", taskId).Scan(&task)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if task.Id == 0 {
			continue
		}
		// 5. 查询该任务在指定 hotKey 中的分数
		score, err := g.Redis().ZScore(ctx, hotKey, taskId) // 获取该任务在指定 hotKey 中的分数
		if err != nil {
			return nil, err
		}
		items = append(items, taskV1.HotTaskItem{
			TaskId:    task.Id,
			Title:     task.Title,
			Status:    task.Status,
			Priority:  task.Priority,
			ViewCount: uint64(score),
		})
	}
	return items, nil
}

// taskHotWeeklyKey 生成团队热门任务周榜 key。
func taskHotWeeklyKey(teamId uint64, now time.Time) string {
	// 1. 从 now 计算 ISO 年份和周数
	year, week := now.ISOWeek() // 获取 ISO 年份和周数
	// 2. 返回 team:task:hot:weekly:{teamId}:{year}-{week}
	return fmt.Sprintf("team:task:hot:weekly:%d:%d-%02d", teamId, year, week)
}
