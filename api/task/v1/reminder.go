package v1

import "github.com/gogf/gf/v2/frame/g"

type SetReminderReq struct {
	// 1. 定义 POST /tasks/{taskId}/reminder
	g.Meta `path:"/tasks/{taskId}/reminder" method:"post" tags:"Task" summary:"设置任务提醒"`
	// 2. taskId 从 path 获取
	TaskId uint64 `json:"taskId" in:"path" v:"required|min:1#任务ID不能为空|任务ID不合法"`
	// 3. remindAt 从 body 获取，Unix 秒级时间戳
	RemindAt int64 `json:"remindAt" v:"required|min:1#提醒时间不能为空|提醒时间不合法"`
}

type SetReminderRes struct{}

type CancelReminderReq struct {
	g.Meta `path:"/tasks/{taskId}/reminder" method:"delete" tags:"Task" summary:"取消任务提醒"`
	TaskId uint64 `json:"taskId" in:"path" v:"required|min:1#任务ID不能为空|任务ID不合法"`
}

type CancelReminderRes struct{}
