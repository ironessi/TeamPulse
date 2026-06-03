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

	// 2. SADD task:likes:{taskId} userId
	if _, err = g.Redis().Do(ctx, "SADD", taskLikeKey(taskId), userId); err != nil {
		return 0, err
	}

	// 3. SCARD 返回最新点赞数
	value, err := g.Redis().Do(ctx, "SCARD", taskLikeKey(taskId))
	if err != nil {
		return 0, err
	}
	return value.Uint64(), nil
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
