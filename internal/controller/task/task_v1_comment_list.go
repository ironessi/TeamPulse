package task

import (
	"context"
	v1 "redis-demo/api/task/v1"
	taskLogic "redis-demo/internal/logic/task"
	"redis-demo/internal/middleware"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
)

func (c *ControllerV1) ListComments(ctx context.Context, req *v1.ListCommentsReq) (res *v1.ListCommentsRes, err error) {
	// 1. 从 JWT 上下文读取当前 userId
	userId := g.RequestFromCtx(ctx).GetCtxVar(middleware.ContextUserId).Uint64()
	// 2. userId 为 0 时返回“请先登录”
	if userId == 0 {
		return nil, gerror.New("请先登录")
	}
	commentItems, err := taskLogic.ListComments(ctx, userId, req.TaskId)
	if err != nil {
		return nil, err
	}
	return &v1.ListCommentsRes{
		List: commentItems,
	}, nil
}
