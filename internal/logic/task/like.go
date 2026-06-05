package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"redis-demo/internal/dao"
	"redis-demo/internal/model/entity"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
)

const likeTaskLuaScript = `
local added = redis.call("SADD", KEYS[1], ARGV[1])
local count = redis.call("SCARD", KEYS[1])
return {added, count}
`
const likeTaskTransactionMaxRetry = 3

// taskLikeKey 生成任务点赞 Set key。
func taskLikeKey(taskId uint64) string {
	// 返回 task:likes:{taskId}
	return fmt.Sprintf("task:likes:%d", taskId)
}

// LikeTask 点赞任务。
func LikeTask(ctx context.Context, userId uint64, taskId uint64) (uint64, error) {
	// 1. 校验任务存在，并校验用户属于任务团队
	var task entity.Task
	err := dao.Task.Ctx(ctx).Where("id", taskId).Scan(&task)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, gerror.New("任务不存在")
	}
	if err != nil {
		return 0, err
	}
	if task.Id == 0 {
		return 0, gerror.New("任务不存在")
	}

	count, err := dao.TeamMember.Ctx(ctx).Where("team_id", task.TeamId).Where("user_id", userId).Count()
	if err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, gerror.New("用户不属于任务团队")
	}

	// 2. 用 Lua 把 SADD 和 SCARD 合成一次 Redis 原子操作。
	// Redis 执行 Lua 时不会被其他命令插入，因此 added 和 likeCount 来自同一次连续执行。
	value, err := g.Redis().Do(ctx, "EVAL", likeTaskLuaScript, 1, taskLikeKey(taskId), userId)
	if err != nil {
		return 0, err
	}

	// 3. Lua 返回 {added, likeCount}，当前接口只需要返回 likeCount。
	values := value.Vars()
	if len(values) < 2 {
		return 0, gerror.New("Lua 脚本执行错误")
	}
	return values[1].Uint64(), nil
}

// UnlikeTask 取消点赞任务。
func UnlikeTask(ctx context.Context, userId uint64, taskId uint64) (uint64, error) {
	// 1. 校验任务存在，并校验用户属于任务团队
	var task entity.Task
	err := dao.Task.Ctx(ctx).Where("id", taskId).Scan(&task)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, gerror.New("任务不存在")
	}
	if err != nil {
		return 0, err
	}
	if task.Id == 0 {
		return 0, gerror.New("任务不存在")
	}

	count, err := dao.TeamMember.Ctx(ctx).Where("team_id", task.TeamId).Where("user_id", userId).Count()
	if err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, gerror.New("用户不属于任务团队")
	}
	// 2. SREM task:likes:{taskId} userId
	if _, err = g.Redis().Do(ctx, "SREM", taskLikeKey(taskId), userId); err != nil {
		return 0, err
	}
	// 3. SCARD 返回最新点赞数
	value, err := g.Redis().Do(ctx, "SCARD", taskLikeKey(taskId))
	if err != nil {
		return 0, err
	}
	return value.Uint64(), nil
}

// GetLikeStatus 查询当前用户点赞状态。
func GetLikeStatus(ctx context.Context, userId uint64, taskId uint64) (bool, uint64, error) {
	// 1. 校验任务存在，并校验用户属于任务团队
	var task entity.Task
	err := dao.Task.Ctx(ctx).Where("id", taskId).Scan(&task)
	if errors.Is(err, sql.ErrNoRows) {
		return false, 0, gerror.New("任务不存在")
	}
	if err != nil {
		return false, 0, err
	}
	if task.Id == 0 {
		return false, 0, gerror.New("任务不存在")
	}

	count, err := dao.TeamMember.Ctx(ctx).Where("team_id", task.TeamId).Where("user_id", userId).Count()
	if err != nil {
		return false, 0, err
	}
	if count == 0 {
		return false, 0, gerror.New("用户不属于任务团队")
	}
	// 2. SISMEMBER 查询当前用户是否点赞
	likedValue, err := g.Redis().Do(ctx, "SISMEMBER", taskLikeKey(taskId), userId)
	if err != nil {
		return false, 0, err
	}

	// 3. SCARD 查询点赞数
	countValue, err := g.Redis().Do(ctx, "SCARD", taskLikeKey(taskId))
	if err != nil {
		return false, 0, err
	}
	return likedValue.Int() == 1, countValue.Uint64(), nil
}

func LikeTaskWithRedisTransaction(ctx context.Context, userId uint64, taskId uint64) (uint64, error) {
	// 1. 校验任务存在
	var task entity.Task
	err := dao.Task.Ctx(ctx).Where("id", taskId).Scan(&task)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, gerror.New("任务不存在")
	}
	if err != nil {
		return 0, err
	}
	if task.Id == 0 {
		return 0, gerror.New("任务不存在")
	}
	// 2. 校验当前用户属于任务团队
	count, err := dao.TeamMember.Ctx(ctx).Where("team_id", task.TeamId).Where("user_id", userId).Count()
	if err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, gerror.New("用户不属于任务团队")
	}

	// 3. 生成点赞 key
	likeKey := taskLikeKey(taskId)

	// 4. 最多重试 3 次
	for i := 0; i < likeTaskTransactionMaxRetry; i++ {
		// 4.1 WATCH 点赞 key
		if _, err := g.Redis().Do(ctx, "WATCH", likeKey); err != nil {
			return 0, err
		}
		// 4.2 MULTI 开启事务
		if _, err := g.Redis().Do(ctx, "MULTI"); err != nil {
			// MULTI 失败时事务尚未开始，取消 WATCH，避免影响后续命令。
			_, _ = g.Redis().Do(ctx, "UNWATCH")
			return 0, err
		}
		// 4.3 SADD 点赞
		if _, err = g.Redis().Do(ctx, "SADD", likeKey, userId); err != nil {
			// MULTI 之后出错时，丢弃已经排队的事务命令并退出事务状态。
			_, _ = g.Redis().Do(ctx, "DISCARD")
			return 0, err
		}
		// 4.4 SCARD 查询点赞数
		if _, err = g.Redis().Do(ctx, "SCARD", likeKey); err != nil {
			// MULTI 之后出错时，丢弃已经排队的事务命令并退出事务状态。
			_, _ = g.Redis().Do(ctx, "DISCARD")
			return 0, err
		}
		// 4.5 EXEC 提交事务
		value, err := g.Redis().Do(ctx, "EXEC")
		if err != nil {
			return 0, err
		}
		// 4.6 如果 EXEC 成功，解析 SCARD 的结果并返回
		values := value.Vars()
		// EXEC 成功时会按事务命令顺序返回结果：values[0] 是 SADD，values[1] 是 SCARD。
		if len(values) >= 2 {
			return values[1].Uint64(), nil
		}
		// 4.7 如果 EXEC 失败，说明 WATCH 期间 key 被修改，继续重试

		// 5. 多次重试失败后，返回错误
	}
	return 0, gerror.New("点赞事务提交失败，请重试")
}
