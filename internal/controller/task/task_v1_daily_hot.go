package task

import (
	"context"
	v1 "redis-demo/api/task/v1"
	taskLogic "redis-demo/internal/logic/task"
	"redis-demo/internal/middleware"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
)

// DailyHot 查询指定团队的今日热门任务排行榜。
func (c *ControllerV1) DailyHot(ctx context.Context, req *v1.DailyHotReq) (res *v1.DailyHotRes, err error) {
	// 1. 从 JWT 上下文读取 userId
	userId := g.RequestFromCtx(ctx).GetCtxVar(middleware.ContextUserId).Uint64()
	// 2. userId 为 0 时返回请先登录
	if userId == 0 {
		return nil, gerror.New("请先登录")
	}
	// 3. 调用 taskLogic.GetDailyHotTasks
	tasks, err := taskLogic.GetDailyHotTasks(ctx, userId, req.TeamId, time.Now())
	if err != nil {
		return nil, err
	}
	// 4. 包装响应返回
	return &v1.DailyHotRes{
		Tasks: tasks,
	}, nil
}
