package task

import (
	"context"
	v1 "redis-demo/api/task/v1"
	taskLogic "redis-demo/internal/logic/task"
	"redis-demo/internal/middleware"

	"github.com/gogf/gf/v2/frame/g"
)

// CreateComment 创建任务评论。
func (c *ControllerV1) CreateComment(ctx context.Context, req *v1.CreateCommentReq) (res *v1.CreateCommentRes, err error) {
	// 1. 从 ctx 获取当前登录用户 ID
	userId := g.RequestFromCtx(ctx).GetCtxVar(middleware.ContextUserId).Uint64()
	// 2. 调用 task logic 创建评论
	commentId, err := taskLogic.CreateComment(ctx, userId, req.TaskId, req.Content, req.MentionUserIds)
	if err != nil {
		return nil, err
	}
	// 3. 返回 commentId
	return &v1.CreateCommentRes{CommentId: uint64(commentId)}, nil
}
