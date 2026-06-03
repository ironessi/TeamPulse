package v1

import "github.com/gogf/gf/v2/frame/g"

type LikeTaskReq struct {
	// 1. 定义 POST /tasks/{taskId}/like
	g.Meta `path:"/tasks/{taskId}/like" method:"post" tags:"Task" summary:"点赞任务"`
	// 2. taskId 从 path 获取
	TaskId uint64 `json:"taskId" in:"path" v:"required|min:1#任务ID不能为空|任务ID不合法"`
}

type LikeTaskRes struct {
	// 1. 返回当前点赞数
	LikeCount uint64 `json:"likeCount"`
}

type UnlikeTaskReq struct {
	// 1. 定义 DELETE /tasks/{taskId}/like
	g.Meta `path:"/tasks/{taskId}/like" method:"delete" tags:"Task" summary:"取消点赞任务"`
	// 2. taskId 从 path 获取
	TaskId uint64 `json:"taskId" in:"path" v:"required|min:1#任务ID不能为空|任务ID不合法"`
}

type UnlikeTaskRes struct {
	// 1. 返回当前点赞数
	LikeCount uint64 `json:"likeCount"`
}

type LikeStatusReq struct {
	// 1. 定义 GET /tasks/{taskId}/like-status
	g.Meta `path:"/tasks/{taskId}/like-status" method:"get" tags:"Task" summary:"查询任务点赞状态"`
	// 2. taskId 从 path 获取
	TaskId uint64 `json:"taskId" in:"path" v:"required|min:1#任务ID不能为空|任务ID不合法"`
}

type LikeStatusRes struct {
	// 1. 当前用户是否已点赞
	IsLiked bool `json:"isLiked"`
	// 2. 当前任务点赞数
	LikeCount uint64 `json:"likeCount"`
}
