package task

import (
	"context"
	v1 "redis-demo/api/task/v1"
	taskLogic "redis-demo/internal/logic/task"
	"redis-demo/internal/middleware"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
)

func (c *ControllerV1) SetReminder(ctx context.Context, req *v1.SetReminderReq) (res *v1.SetReminderRes, err error) {
	// 1. 从 JWT 上下文读取 operatorId
	operatorId := g.RequestFromCtx(ctx).GetCtxVar(middleware.ContextUserId).Uint64()

	// 2. 未登录时返回“请先登录”
	if operatorId == 0 {
		return nil, gerror.New("请先登录")
	}
	// 3. 调用 taskLogic.SetReminder(ctx, operatorId, req.TaskId, req.RemindAt)
	err = taskLogic.SetReminder(ctx, operatorId, req.TaskId, req.RemindAt)
	if err != nil {
		return nil, err
	}
	return &v1.SetReminderRes{}, nil
}
