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

// WeeklyHot 查询指定团队的周榜热门任务。
func (c *ControllerV1) WeeklyHot(ctx context.Context, req *v1.WeeklyHotReq) (res *v1.WeeklyHotRes, err error) {
	userId := g.RequestFromCtx(ctx).GetCtxVar(middleware.ContextUserId).Uint64()
	if userId == 0 {
		return nil, gerror.New("请先登录")
	}
	tasks, err := taskLogic.GetWeeklyHotTasks(ctx, userId, req.TeamId, time.Now())
	if err != nil {
		return nil, err
	}
	return &v1.WeeklyHotRes{
		Tasks: tasks,
	}, nil
}
