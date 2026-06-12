package v1

import "github.com/gogf/gf/v2/frame/g"

// CreateReq 是创建任务请求。
// teamId 来自路径参数，creatorId 从 JWT 鉴权上下文获取。
type CreateReq struct {
	g.Meta      `path:"/teams/{teamId}/tasks" method:"post" tags:"Task" summary:"创建任务"`
	TeamId      uint64 `json:"teamId" v:"required|min:1#团队ID不能为空|团队ID不合法"`
	Title       string `json:"title" v:"required|min-length:1#请输入任务标题|任务标题至少1个字符"`
	Description string `json:"description" v:"required#请输入任务描述"`
	AssigneeId  uint64 `json:"assigneeId" v:"min:0#受托人ID不合法"`
	Priority    uint   `json:"priority" v:"required|in:1,2,3#请选择任务优先级|任务优先级必须是1(低)、2(中)、3(高)"`
}

// CreateRes 是创建任务响应。
type CreateRes struct {
	TaskId uint64 `json:"taskId"`
}

// ListReq 是查询团队任务列表请求。
type ListReq struct {
	g.Meta `path:"/teams/{teamId}/tasks" method:"get" tags:"Task" summary:"查询任务列表"`
	TeamId uint64 `json:"teamId" v:"required|min:1#团队ID不能为空|团队ID不合法"`
}

// TaskItem 是任务列表中的一条任务。
type TaskItem struct {
	TaskId      uint64 `json:"taskId"`
	CreatorId   uint64 `json:"creatorId"`
	Title       string `json:"title"`
	Description string `json:"description"`
	AssigneeId  uint64 `json:"assigneeId"`
	Priority    int    `json:"priority"`
	Status      string `json:"status"`
}

// ListRes 是查询团队任务列表响应。
type ListRes struct {
	Tasks []TaskItem `json:"tasks"`
}

// UpdateReq 是更新任务基本信息请求。
// 状态修改由独立的 UpdateStatusReq 处理。
type UpdateReq struct {
	g.Meta      `path:"/tasks/{taskId}" method:"put" tags:"Task" summary:"更新任务信息"`
	TaskId      uint64 `json:"taskId" v:"required|min:1#任务ID不能为空|任务ID不合法"`
	Title       string `json:"title" v:"required|min-length:1#请输入任务标题|任务标题至少1个字符"`
	Description string `json:"description" v:"required#请输入任务描述"`
	AssigneeId  uint64 `json:"assigneeId" v:"min:0#受托人ID不合法"`
	Priority    uint   `json:"priority" v:"required|in:1,2,3#请选择任务优先级|任务优先级必须是1(低)、2(中)、3(高)"`
}

// UpdateRes 是更新任务基本信息响应。
type UpdateRes struct{}

// UpdateStatusReq 是更新任务状态请求。
// taskId 来自路径参数，操作者从 JWT 鉴权上下文获取。
type UpdateStatusReq struct {
	g.Meta `path:"/tasks/{taskId}/status" method:"patch" tags:"Task" summary:"更新任务状态"`
	TaskId uint64 `json:"taskId" v:"required|min:1#任务ID不能为空|任务ID不合法"`
	Status string `json:"status" v:"required|in:todo,doing,done#请选择任务状态|任务状态必须是todo、doing或done"`
}

// UpdateStatusRes 是更新任务状态响应。
type UpdateStatusRes struct{}

// DetailReq 是查询任务详情请求。
// taskId 来自路径参数，当前用户从 JWT 鉴权上下文获取。
type DetailReq struct {
	g.Meta `path:"/tasks/{taskId}" method:"get" tags:"Task" summary:"查询任务详情"`
	TaskId uint64 `json:"taskId" v:"required|min:1#任务ID不能为空|任务ID不合法"`
}

// DetailRes 是查询任务详情响应。
type DetailRes struct {
	Task TaskItem `json:"task"`
}

// HotReq 是查询团队热门任务请求。
// teamId 来自路径参数，当前用户从 JWT 鉴权上下文获取。
type HotReq struct {
	g.Meta `path:"/teams/{teamId}/tasks/hot" method:"get" tags:"Task" summary:"热门任务排行"`
	TeamId uint64 `json:"teamId" v:"required|min:1#团队ID不能为空|团队ID不合法"`
}

// HotTaskItem 是热门任务列表项。
type HotTaskItem struct {
	TaskId    uint64 `json:"taskId"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Priority  int    `json:"priority"`
	ViewCount uint64 `json:"viewCount"`
}

// HotRes 是查询热门任务排行响应。
type HotRes struct {
	Tasks []HotTaskItem `json:"tasks"`
}

// DailyHotReq 是查询团队今日热门任务请求。
type DailyHotReq struct {
	// GET /teams/{teamId}/tasks/hot/daily
	g.Meta `path:"/teams/{teamId}/tasks/hot/daily" method:"get" tags:"Task" summary:"查询团队今日热门任务"`
	// teamId 路径参数
	TeamId uint64 `json:"teamId" v:"required|min:1#团队ID不能为空|团队ID不合法"`
}

// DailyHotRes 是查询团队今日热门任务响应。
type DailyHotRes struct {
	// 复用 HotTaskItem
	Tasks []HotTaskItem `json:"tasks"`
}

// WeeklyHotReq 是查询团队本周热门任务请求。
type WeeklyHotReq struct {
	g.Meta `path:"/teams/{teamId}/tasks/hot/weekly" method:"get" tags:"Task" summary:"查询团队本周热门任务"`
	// TODO: teamId 路径参数，必填，最小值 1
	TeamId uint64 `json:"teamId" v:"required|min:1#团队ID不能为空|团队ID不合法"`
}

// WeeklyHotRes 是查询团队本周热门任务响应。
type WeeklyHotRes struct {
	// TODO: 复用 HotTaskItem
	Tasks []HotTaskItem `json:"tasks"`
}
