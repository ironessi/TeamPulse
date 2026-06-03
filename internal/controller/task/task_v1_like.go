package task

import (
	"context"
	v1 "redis-demo/api/task/v1"
	taskLogic "redis-demo/internal/logic/task"
	"redis-demo/internal/middleware"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
)

func (c *ControllerV1) LikeTask(ctx context.Context, req *v1.LikeTaskReq) (res *v1.LikeTaskRes, err error) {
	// 1. 从 JWT 上下文读取 userId
	userId := g.RequestFromCtx(ctx).GetCtxVar(middleware.ContextUserId).Uint64()
	// 2. 未登录返回“请先登录”
	if userId == 0 {
		return nil, gerror.New("请先登录")
	}
	// 3. 调用 taskLogic.LikeTask
	likeCount, err := taskLogic.LikeTask(ctx, userId, req.TaskId)
	// 4. 返回点赞数
	if err != nil {
		return nil, err
	}
	return &v1.LikeTaskRes{LikeCount: likeCount}, nil
}

func (c *ControllerV1) UnlikeTask(ctx context.Context, req *v1.UnlikeTaskReq) (res *v1.UnlikeTaskRes, err error) {
	userId := g.RequestFromCtx(ctx).GetCtxVar(middleware.ContextUserId).Uint64()
	if userId == 0 {
		return nil, gerror.New("请先登录")
	}

	likedCount, err := taskLogic.UnlikeTask(ctx, userId, req.TaskId)
	if err != nil {
		return nil, err
	}
	return &v1.UnlikeTaskRes{
		LikeCount: likedCount,
	}, nil
}
func (c *ControllerV1) LikeStatus(ctx context.Context, req *v1.LikeStatusReq) (res *v1.LikeStatusRes, err error) {
	userId := g.RequestFromCtx(ctx).GetCtxVar(middleware.ContextUserId).Uint64()
	if userId == 0 {
		return nil, gerror.New("请先登录")
	}

	likeStatus, likeCount, err := taskLogic.GetLikeStatus(ctx, userId, req.TaskId)
	if err != nil {
		return nil, err
	}
	return &v1.LikeStatusRes{
		IsLiked:   likeStatus,
		LikeCount: likeCount,
	}, nil
}
