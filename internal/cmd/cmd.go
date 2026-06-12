package cmd

import (
	"context"
	"redis-demo/internal/controller/auth"
	"redis-demo/internal/controller/notification"
	"redis-demo/internal/controller/presence"
	"redis-demo/internal/controller/task"
	"redis-demo/internal/controller/team"
	"redis-demo/internal/controller/user"
	notificationLogic "redis-demo/internal/logic/notification"
	taskLogic "redis-demo/internal/logic/task"
	"redis-demo/internal/middleware"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gcmd"
)

var (
	Main = gcmd.Command{
		Name:  "main",
		Usage: "main",
		Brief: "start http server",
		Func: func(ctx context.Context, parser *gcmd.Parser) (err error) {
			s := g.Server()

			// 启动任务提醒扫描器。它会定时扫描 Redis ZSet 中已经到期的任务提醒。
			taskLogic.StartReminderScanner(ctx)

			// 2. 启动通知重试 worker
			notificationLogic.StartNotificationRetryWorker(ctx)

			// 启动 Stream 通知事件 worker。
			notificationLogic.StartNotificationEventWorker(ctx)

			// 启动后台热度衰减 worker，定期降低排行榜分数。
			taskLogic.StartHeatDecayWorker(ctx)

			// 将 resource/public 作为静态资源目录，浏览器访问 / 时会加载前端页面。
			s.SetServerRoot("resource/public")
			s.Group("/", func(group *ghttp.RouterGroup) {
				group.Middleware(ghttp.MiddlewareHandlerResponse)
				// auth 相关接口不需要登录，例如注册和登录。
				group.Bind(
					auth.NewV1(),
				)
				// user 相关接口需要先通过 JWT 鉴权。
				group.Group("/", func(group *ghttp.RouterGroup) {
					group.Middleware(middleware.Auth) // JWT 鉴权中间件，验证用户身份并把用户信息写入请求上下文
					group.Bind(
						user.NewV1(),
						team.NewV1(),
						presence.NewV1(),
						task.NewV1(),
						notification.NewV1(),
					)
				})
			})
			s.Run()
			return nil
		},
	}
)
