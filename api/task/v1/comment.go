package v1

import "github.com/gogf/gf/v2/frame/g"

// CreateCommentReq 创建任务评论请求。
type CreateCommentReq struct {
	// 1. 路由方法和路径
	g.Meta `path:"/tasks/{taskId}/comments" method:"post" tags:"Comment" summary:"创建任务评论"`
	// 2. taskId 从路径参数获取
	TaskId uint64 `json:"taskId" in:"path" v:"required|min:1#任务ID不能为空|任务ID不合法"`
	// 3. content 从请求 body 获取，并做必填校验
	Content string `json:"content" v:"required#请输入评论内容"`
	// 4. mentionUserIds 可选，用于后续创建提及通知
	MentionUserIds []uint64 `json:"mentionUserIds"`
}

// CreateCommentRes 创建任务评论响应。
type CreateCommentRes struct {
	// 1. 返回新评论 ID
	CommentId uint64 `json:"commentId"`
}

// ListCommentsReq 查询任务评论列表请求。
type ListCommentsReq struct {
	// 1. 路由方法和路径
	g.Meta `path:"/tasks/{taskId}/comments" method:"get" tags:"Comment" summary:"查询任务评论列表"`
	// 2. taskId 从路径参数获取
	TaskId uint64 `json:"taskId" in:"path" v:"required|min:1#任务ID不能为空|任务ID不合法"`
}

// ListCommentsRes 查询任务评论列表响应。
type ListCommentsRes struct {
	// 1. 评论列表
	List []CommentItem `json:"list"`
}

// CommentItem 评论列表项。
type CommentItem struct {
	// 1. 评论 ID
	CommentId uint64 `json:"commentId"`
	// 2. 任务 ID
	TaskId uint64 `json:"taskId"`
	// 3. 团队 ID
	TeamId uint64 `json:"teamId"`
	// 4. 评论用户 ID
	UserId uint64 `json:"userId"`
	// 5. 评论内容
	Content string `json:"content"`
	// 6. 创建时间 Unix 秒时间戳
	CreatedAt int64 `json:"createdAt"`
}
