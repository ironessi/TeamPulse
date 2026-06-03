package task

import (
	"context"
	v1 "redis-demo/api/task/v1"
	taskLogic "redis-demo/internal/logic/task"
	"redis-demo/internal/middleware"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
)

func (c *ControllerV1) CancelReminder(ctx context.Context, req *v1.CancelReminderReq) (res *v1.CancelReminderRes, err error) {
	// 1. 从 JWT 上下文读取 operatorId
	operatorId := g.RequestFromCtx(ctx).GetCtxVar(middleware.ContextUserId).Uint64()
	// 2. 未登录时返回“请先登录”
	if operatorId == 0 {
		return nil, gerror.New("请先登录")
	}
	// 3. 调用 taskLogic.CancelReminder(ctx, operatorId, req.TaskId)
	err = taskLogic.CancelReminder(ctx, operatorId, req.TaskId)
	if err != nil {
		return nil, err
	}
	// 4. 返回空响应
	return &v1.CancelReminderRes{}, nil
}
