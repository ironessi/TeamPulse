package task

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"redis-demo/internal/dao"
	notificationLogic "redis-demo/internal/logic/notification"
	"redis-demo/internal/model/entity"

	_ "github.com/gogf/gf/contrib/drivers/mysql/v2"
	_ "github.com/gogf/gf/contrib/nosql/redis/v2"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gctx"
)

func TestGetTaskCachesNullForMissingTask(t *testing.T) {
	ctx := gctx.New()
	missingTaskId := uint64(time.Now().UnixNano())
	key := taskDetailCacheKey(missingTaskId)

	t.Cleanup(func() {
		if _, err := g.Redis().Del(ctx, key); err != nil {
			t.Errorf("clean task detail null cache failed: %v", err)
		}
	})

	_, err := GetTask(ctx, 1, missingTaskId)
	if err == nil {
		t.Fatal("missing task should return error")
	}
	if !strings.Contains(err.Error(), "任务不存在") {
		t.Fatalf("unexpected missing task error: %v", err)
	}

	value, err := g.Redis().Get(ctx, key)
	if err != nil {
		t.Fatalf("read null cache failed: %v", err)
	}
	if value.String() != taskDetailCacheNullValue {
		t.Fatalf("unexpected null cache value: %q", value.String())
	}

	ttl, err := g.Redis().TTL(ctx, key)
	if err != nil {
		t.Fatalf("read null cache ttl failed: %v", err)
	}
	if ttl <= 0 {
		t.Fatalf("null cache should have ttl, got %d", ttl)
	}

	_, err = GetTask(ctx, 1, missingTaskId)
	if err == nil {
		t.Fatal("missing task cached as null should still return error")
	}
	if !strings.Contains(err.Error(), "任务不存在") {
		t.Fatalf("unexpected cached missing task error: %v", err)
	}
}

func TestGetTaskCachesExistingTaskAndKeepsPermissionCheck(t *testing.T) {
	ctx := gctx.New()
	task, memberId, outsiderId := updateTaskTestFixture(t)
	key := taskDetailCacheKey(task.Id)

	if _, err := g.Redis().Del(ctx, key); err != nil {
		t.Fatalf("clean task detail cache before test failed: %v", err)
	}

	t.Cleanup(func() {
		if _, err := g.Redis().Del(ctx, key); err != nil {
			t.Errorf("clean task detail cache after test failed: %v", err)
		}
		if _, err := g.Redis().ZIncrBy(ctx, taskHotKey(task.TeamId), -1, task.Id); err != nil {
			t.Errorf("restore heat after member detail read failed: %v", err)
		}
	})

	item, err := GetTask(ctx, memberId, task.Id)
	if err != nil {
		t.Fatalf("member get task failed: %v", err)
	}
	if item.TaskId != task.Id {
		t.Fatalf("unexpected task item: %+v", item)
	}

	value, err := g.Redis().Get(ctx, key)
	if err != nil {
		t.Fatalf("read task detail cache failed: %v", err)
	}
	if value.IsNil() || value.String() == taskDetailCacheNullValue {
		t.Fatalf("task detail cache should contain task json, got %q", value.String())
	}

	var cached entity.Task
	if err := json.Unmarshal([]byte(value.String()), &cached); err != nil {
		t.Fatalf("task detail cache should be json: %v", err)
	}
	if cached.Id != task.Id || cached.TeamId != task.TeamId {
		t.Fatalf("unexpected cached task: %+v", cached)
	}

	_, err = GetTask(ctx, outsiderId, task.Id)
	if err == nil {
		t.Fatal("cached task should still reject outsider")
	}
	if !strings.Contains(err.Error(), "你没有权限查看该任务") {
		t.Fatalf("unexpected outsider error: %v", err)
	}
}

func TestUpdateTaskDeletesTaskDetailCache(t *testing.T) {
	ctx := gctx.New()
	original, operatorId, _ := updateTaskTestFixture(t)
	key := taskDetailCacheKey(original.Id)
	activityKey := fmt.Sprintf("team:activities:%d", original.TeamId)

	if err := g.Redis().SetEX(ctx, key, `{"cached":"old"}`, int64(taskDetailCacheExpire.Seconds())); err != nil {
		t.Fatalf("prepare task detail cache failed: %v", err)
	}

	updatedTitle := original.Title + "-cache-delete"
	updatedDescription := original.Description + "-cache-delete"

	err := UpdateTask(
		ctx,
		operatorId,
		original.Id,
		updatedTitle,
		updatedDescription,
		original.AssigneeId,
		uint(original.Priority),
	)
	if err != nil {
		t.Fatalf("UpdateTask failed: %v", err)
	}

	t.Cleanup(func() {
		restoreTaskEditableFields(t, original)
		if _, err := g.Redis().Del(ctx, key); err != nil {
			t.Errorf("clean task detail cache failed: %v", err)
		}
		if _, err := g.Redis().LPop(ctx, activityKey); err != nil {
			t.Errorf("remove test activity failed: %v", err)
		}
		if _, err := g.Redis().ZIncrBy(ctx, taskHotKey(original.TeamId), -1, original.Id); err != nil {
			t.Errorf("restore heat failed: %v", err)
		}
	})

	value, err := g.Redis().Get(ctx, key)
	if err != nil {
		t.Fatalf("read task detail cache failed: %v", err)
	}
	if !value.IsNil() {
		t.Fatalf("task detail cache should be deleted after update, got %q", value.String())
	}
}

func TestUpdateStatusDeletesTaskDetailCache(t *testing.T) {
	ctx := gctx.New()
	task, operatorId, _ := statusNotificationTestFixture(t)
	key := taskDetailCacheKey(task.Id)
	activityKey := fmt.Sprintf("team:activities:%d", task.TeamId)
	newStatus := nextStatus(task.Status)

	if err := g.Redis().SetEX(ctx, key, `{"cached":"old"}`, int64(taskDetailCacheExpire.Seconds())); err != nil {
		t.Fatalf("prepare task detail cache failed: %v", err)
	}

	err := UpdateStatus(ctx, operatorId, task.Id, newStatus)
	if err != nil {
		t.Fatalf("UpdateStatus failed: %v", err)
	}

	t.Cleanup(func() {
		restoreTaskForStatusTest(t, task)
		if _, err := g.Redis().Del(ctx, key); err != nil {
			t.Errorf("clean task detail cache failed: %v", err)
		}
		if _, err := g.Redis().LPop(ctx, activityKey); err != nil {
			t.Errorf("remove test activity failed: %v", err)
		}
		if _, err := g.Redis().ZIncrBy(ctx, taskHotKey(task.TeamId), -1, task.Id); err != nil {
			t.Errorf("restore heat failed: %v", err)
		}
	})

	value, err := g.Redis().Get(ctx, key)
	if err != nil {
		t.Fatalf("read task detail cache failed: %v", err)
	}
	if !value.IsNil() {
		t.Fatalf("task detail cache should be deleted after status update, got %q", value.String())
	}
}

func TestTaskDetailCacheTTLHasJitterRange(t *testing.T) {
	minTTL := taskDetailCacheExpire
	maxTTL := taskDetailCacheExpire + taskDetailCacheJitterMax

	seenJitter := false
	for i := 0; i < 100; i++ {
		ttl := taskDetailCacheTTL()
		if ttl < minTTL || ttl > maxTTL {
			t.Fatalf("task detail cache ttl out of range: got=%s min=%s max=%s", ttl, minTTL, maxTTL)
		}
		if ttl > minTTL {
			seenJitter = true
		}
	}

	if !seenJitter {
		t.Fatal("task detail cache ttl should include random jitter")
	}
}

func TestGetTaskReleasesDetailLockAfterCacheRebuild(t *testing.T) {
	ctx := gctx.New()
	task, memberId, _ := updateTaskTestFixture(t)
	cacheKey := taskDetailCacheKey(task.Id)
	lockKey := taskDetailLockKey(task.Id)

	if _, err := g.Redis().Del(ctx, cacheKey); err != nil {
		t.Fatalf("clean task detail cache before test failed: %v", err)
	}
	if _, err := g.Redis().Del(ctx, lockKey); err != nil {
		t.Fatalf("clean task detail lock before test failed: %v", err)
	}

	t.Cleanup(func() {
		if _, err := g.Redis().Del(ctx, cacheKey, lockKey); err != nil {
			t.Errorf("clean task detail cache and lock failed: %v", err)
		}
		if _, err := g.Redis().ZIncrBy(ctx, taskHotKey(task.TeamId), -1, task.Id); err != nil {
			t.Errorf("restore heat after detail read failed: %v", err)
		}
	})

	item, err := GetTask(ctx, memberId, task.Id)
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if item.TaskId != task.Id {
		t.Fatalf("unexpected task item: %+v", item)
	}

	value, err := g.Redis().Get(ctx, cacheKey)
	if err != nil {
		t.Fatalf("read rebuilt task detail cache failed: %v", err)
	}
	if value.IsNil() {
		t.Fatal("task detail cache should be rebuilt")
	}

	lockValue, err := g.Redis().Get(ctx, lockKey)
	if err != nil {
		t.Fatalf("read task detail lock after rebuild failed: %v", err)
	}
	if !lockValue.IsNil() {
		t.Fatalf("task detail lock should be released after rebuild, got %q", lockValue.String())
	}
}

func TestCreateCommentCreatesRecordActivityAndListItem(t *testing.T) {
	ctx := gctx.New()
	task, memberId, _ := updateTaskTestFixture(t)
	activityKey := fmt.Sprintf("team:activities:%d", task.TeamId)
	content := fmt.Sprintf("  comment-test-%d  ", time.Now().UnixNano())

	commentId, err := CreateComment(ctx, memberId, task.Id, content, []uint64{memberId})
	if err != nil {
		t.Fatalf("CreateComment failed: %v", err)
	}
	if commentId == 0 {
		t.Fatal("comment id should not be zero")
	}

	t.Cleanup(func() {
		if _, err := dao.TaskComment.Ctx(ctx).Where("id", commentId).Delete(); err != nil {
			t.Errorf("delete test comment failed: %v", err)
		}
		if _, err := g.Redis().LPop(ctx, activityKey); err != nil {
			t.Errorf("remove test comment activity failed: %v", err)
		}
	})

	var created entity.TaskComment
	if err := dao.TaskComment.Ctx(ctx).Where("id", commentId).Scan(&created); err != nil {
		t.Fatalf("query created comment failed: %v", err)
	}
	if created.Id != commentId ||
		created.TaskId != task.Id ||
		created.TeamId != task.TeamId ||
		created.UserId != memberId ||
		created.Content != strings.TrimSpace(content) {
		t.Fatalf("unexpected created comment: %+v", created)
	}

	activityValue, err := g.Redis().LIndex(ctx, activityKey, 0)
	if err != nil {
		t.Fatalf("read comment activity failed: %v", err)
	}
	if !strings.Contains(activityValue.String(), "task_commented") {
		t.Fatalf("comment activity should contain task_commented, got %q", activityValue.String())
	}

	items, err := ListComments(ctx, memberId, task.Id)
	if err != nil {
		t.Fatalf("ListComments failed: %v", err)
	}
	found := false
	for _, item := range items {
		if item.CommentId == commentId {
			found = true
			if item.Content != strings.TrimSpace(content) ||
				item.TaskId != task.Id ||
				item.TeamId != task.TeamId ||
				item.UserId != memberId {
				t.Fatalf("unexpected comment item: %+v", item)
			}
			if item.CreatedAt == 0 {
				t.Fatalf("comment item should include createdAt: %+v", item)
			}
		}
	}
	if !found {
		t.Fatalf("created comment %d not found in list: %+v", commentId, items)
	}
}

func TestCreateCommentRejectsInvalidInputs(t *testing.T) {
	ctx := gctx.New()
	task, memberId, outsiderId := updateTaskTestFixture(t)

	tests := []struct {
		name           string
		userId         uint64
		taskId         uint64
		content        string
		mentionUserIds []uint64
		wantErr        string
	}{
		{
			name:    "empty content",
			userId:  memberId,
			taskId:  task.Id,
			content: "   ",
			wantErr: "请输入评论内容",
		},
		{
			name:    "missing task",
			userId:  memberId,
			taskId:  uint64(time.Now().UnixNano()),
			content: "missing task comment",
			wantErr: "任务不存在",
		},
		{
			name:    "non member",
			userId:  outsiderId,
			taskId:  task.Id,
			content: "outsider comment",
			wantErr: "你没有权限评论该任务",
		},
		{
			name:           "empty mention user",
			userId:         memberId,
			taskId:         task.Id,
			content:        "empty mention",
			mentionUserIds: []uint64{0},
			wantErr:        "提及用户不能为空",
		},
		{
			name:           "mention outside team",
			userId:         memberId,
			taskId:         task.Id,
			content:        "outside mention",
			mentionUserIds: []uint64{outsiderId},
			wantErr:        "提及用户不是该团队成员",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commentId, err := CreateComment(ctx, tt.userId, tt.taskId, tt.content, tt.mentionUserIds)
			if err == nil {
				t.Fatalf("CreateComment should fail, commentId=%d", commentId)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("unexpected error: got=%v want contains=%q", err, tt.wantErr)
			}
		})
	}
}

func TestListCommentsRejectsNonMemberAndMissingTask(t *testing.T) {
	ctx := gctx.New()
	task, _, outsiderId := updateTaskTestFixture(t)

	_, err := ListComments(ctx, outsiderId, task.Id)
	if err == nil {
		t.Fatal("ListComments should reject non-member")
	}
	if !strings.Contains(err.Error(), "你没有权限查看该任务评论") {
		t.Fatalf("unexpected non-member error: %v", err)
	}

	_, err = ListComments(ctx, outsiderId, uint64(time.Now().UnixNano()))
	if err == nil {
		t.Fatal("ListComments should reject missing task")
	}
	if !strings.Contains(err.Error(), "任务不存在") {
		t.Fatalf("unexpected missing task error: %v", err)
	}
}

func TestCreateCommentCreatesMentionNotification(t *testing.T) {
	ctx := gctx.New()
	task, commenterId, mentionUserId := commentNotificationTestFixture(t)
	activityKey := fmt.Sprintf("team:activities:%d", task.TeamId)

	_, err := dao.Task.Ctx(ctx).Where("id", task.Id).Data(g.Map{
		"assignee_id": nil,
	}).Update()
	if err != nil {
		t.Fatalf("prepare task assignee failed: %v", err)
	}

	commentId, err := CreateComment(
		ctx,
		commenterId,
		task.Id,
		fmt.Sprintf("mention notification test %d", time.Now().UnixNano()),
		[]uint64{mentionUserId},
	)
	if err != nil {
		t.Fatalf("CreateComment failed: %v", err)
	}

	var created entity.Notification
	err = dao.Notification.Ctx(ctx).
		Where("receiver_id", mentionUserId).
		Where("actor_id", commenterId).
		Where("type", notificationLogic.TypeTaskMentioned).
		Where("related_task_id", task.Id).
		OrderDesc("id").
		Limit(1).
		Scan(&created)
	if err != nil {
		t.Fatalf("query mention notification failed: %v", err)
	}
	if created.Id == 0 {
		t.Fatal("mention notification was not created")
	}

	inUnreadSet, err := g.Redis().SIsMember(ctx, fmt.Sprintf("notification:unread:%d", mentionUserId), created.Id)
	if err != nil {
		t.Fatalf("check unread set failed: %v", err)
	}
	if inUnreadSet != 1 {
		t.Fatalf("notification %d was not added to unread set", created.Id)
	}

	t.Cleanup(func() {
		restoreTaskForStatusTest(t, task)
		cleanupCommentNotificationTest(t, commentId, created.Id, mentionUserId, activityKey)
	})
}

func TestCreateCommentCreatesAssigneeCommentNotification(t *testing.T) {
	ctx := gctx.New()
	task, commenterId, assigneeId := commentNotificationTestFixture(t)
	activityKey := fmt.Sprintf("team:activities:%d", task.TeamId)

	_, err := dao.Task.Ctx(ctx).Where("id", task.Id).Data(g.Map{
		"assignee_id": assigneeId,
	}).Update()
	if err != nil {
		t.Fatalf("prepare task assignee failed: %v", err)
	}

	commentId, err := CreateComment(
		ctx,
		commenterId,
		task.Id,
		fmt.Sprintf("assignee comment notification test %d", time.Now().UnixNano()),
		nil,
	)
	if err != nil {
		t.Fatalf("CreateComment failed: %v", err)
	}

	var created entity.Notification
	err = dao.Notification.Ctx(ctx).
		Where("receiver_id", assigneeId).
		Where("actor_id", commenterId).
		Where("type", notificationLogic.TypeTaskCommented).
		Where("related_task_id", task.Id).
		OrderDesc("id").
		Limit(1).
		Scan(&created)
	if err != nil {
		t.Fatalf("query assignee comment notification failed: %v", err)
	}
	if created.Id == 0 {
		t.Fatal("assignee comment notification was not created")
	}

	inUnreadSet, err := g.Redis().SIsMember(ctx, fmt.Sprintf("notification:unread:%d", assigneeId), created.Id)
	if err != nil {
		t.Fatalf("check unread set failed: %v", err)
	}
	if inUnreadSet != 1 {
		t.Fatalf("notification %d was not added to unread set", created.Id)
	}

	t.Cleanup(func() {
		restoreTaskForStatusTest(t, task)
		cleanupCommentNotificationTest(t, commentId, created.Id, assigneeId, activityKey)
	})
}

func TestCreateCommentMentionSuppressesDuplicateAssigneeNotification(t *testing.T) {
	ctx := gctx.New()
	task, commenterId, assigneeId := commentNotificationTestFixture(t)
	activityKey := fmt.Sprintf("team:activities:%d", task.TeamId)

	_, err := dao.Task.Ctx(ctx).Where("id", task.Id).Data(g.Map{
		"assignee_id": assigneeId,
	}).Update()
	if err != nil {
		t.Fatalf("prepare task assignee failed: %v", err)
	}

	beforeCommentedCount, err := dao.Notification.Ctx(ctx).
		Where("receiver_id", assigneeId).
		Where("actor_id", commenterId).
		Where("type", notificationLogic.TypeTaskCommented).
		Where("related_task_id", task.Id).
		Count()
	if err != nil {
		t.Fatalf("count comment notifications before test failed: %v", err)
	}

	commentId, err := CreateComment(
		ctx,
		commenterId,
		task.Id,
		fmt.Sprintf("duplicate notification test %d", time.Now().UnixNano()),
		[]uint64{assigneeId},
	)
	if err != nil {
		t.Fatalf("CreateComment failed: %v", err)
	}

	var mentioned entity.Notification
	err = dao.Notification.Ctx(ctx).
		Where("receiver_id", assigneeId).
		Where("actor_id", commenterId).
		Where("type", notificationLogic.TypeTaskMentioned).
		Where("related_task_id", task.Id).
		OrderDesc("id").
		Limit(1).
		Scan(&mentioned)
	if err != nil {
		t.Fatalf("query mention notification failed: %v", err)
	}
	if mentioned.Id == 0 {
		t.Fatal("mention notification was not created")
	}

	afterCommentedCount, err := dao.Notification.Ctx(ctx).
		Where("receiver_id", assigneeId).
		Where("actor_id", commenterId).
		Where("type", notificationLogic.TypeTaskCommented).
		Where("related_task_id", task.Id).
		Count()
	if err != nil {
		t.Fatalf("count comment notifications after test failed: %v", err)
	}
	if afterCommentedCount != beforeCommentedCount {
		t.Fatalf("assignee should not receive duplicate task_commented: before=%d after=%d", beforeCommentedCount, afterCommentedCount)
	}

	t.Cleanup(func() {
		restoreTaskForStatusTest(t, task)
		cleanupCommentNotificationTest(t, commentId, mentioned.Id, assigneeId, activityKey)
	})
}

func TestCreateCommentDoesNotNotifySelf(t *testing.T) {
	ctx := gctx.New()
	task, commenterId, _ := commentNotificationTestFixture(t)
	activityKey := fmt.Sprintf("team:activities:%d", task.TeamId)

	_, err := dao.Task.Ctx(ctx).Where("id", task.Id).Data(g.Map{
		"assignee_id": commenterId,
	}).Update()
	if err != nil {
		t.Fatalf("prepare self assignee failed: %v", err)
	}

	beforeNotifications, err := dao.Notification.Ctx(ctx).
		Where("receiver_id", commenterId).
		Where("actor_id", commenterId).
		WhereIn("type", []string{notificationLogic.TypeTaskCommented, notificationLogic.TypeTaskMentioned}).
		Where("related_task_id", task.Id).
		Count()
	if err != nil {
		t.Fatalf("count self notifications before test failed: %v", err)
	}

	commentId, err := CreateComment(
		ctx,
		commenterId,
		task.Id,
		fmt.Sprintf("self notification test %d", time.Now().UnixNano()),
		[]uint64{commenterId},
	)
	if err != nil {
		t.Fatalf("CreateComment failed: %v", err)
	}

	afterNotifications, err := dao.Notification.Ctx(ctx).
		Where("receiver_id", commenterId).
		Where("actor_id", commenterId).
		WhereIn("type", []string{notificationLogic.TypeTaskCommented, notificationLogic.TypeTaskMentioned}).
		Where("related_task_id", task.Id).
		Count()
	if err != nil {
		t.Fatalf("count self notifications after test failed: %v", err)
	}
	if afterNotifications != beforeNotifications {
		t.Fatalf("self notification should not be created: before=%d after=%d", beforeNotifications, afterNotifications)
	}

	t.Cleanup(func() {
		restoreTaskForStatusTest(t, task)
		cleanupCommentNotificationTest(t, commentId, 0, 0, activityKey)
	})
}

func TestUpdateTaskRejectsNonMember(t *testing.T) {
	ctx := gctx.New()
	task, _, outsiderId := updateTaskTestFixture(t)

	err := UpdateTask(
		ctx,
		outsiderId,
		task.Id,
		"不应该被修改的标题",
		"不应该被修改的说明",
		0,
		2,
	)

	if err == nil {
		t.Fatal("UpdateTask should reject non-member")
	}

	if !strings.Contains(err.Error(), "你没有权限修改该任务") {
		t.Fatalf("unexpected error: %v", err)
	}
}
func TestUpdateTaskRejectsAssigneeOutsideTeam(t *testing.T) {
	ctx := gctx.New()
	task, operatorId, outsiderId := updateTaskTestFixture(t)

	err := UpdateTask(
		ctx,
		operatorId,
		task.Id,
		"不会实际保存的标题",
		"不会实际保存的说明",
		outsiderId,
		2,
	)

	if err == nil {
		t.Fatal("UpdateTask should reject assignee outside team")
	}

	if !strings.Contains(err.Error(), "负责人不是该团队成员") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdateTaskUnchangedDoesNotAddActivityOrHeat(t *testing.T) {
	ctx := gctx.New()
	current, operatorId, _ := updateTaskTestFixture(t)

	activityKey := fmt.Sprintf("team:activities:%d", current.TeamId)

	beforeScore, err := g.Redis().ZScore(ctx, taskHotKey(current.TeamId), current.Id)
	if err != nil {
		t.Fatalf("read heat before update failed: %v", err)
	}

	beforeCount, err := g.Redis().LLen(ctx, activityKey)
	if err != nil {
		t.Fatalf("read activity count before update failed: %v", err)
	}

	err = UpdateTask(
		ctx,
		operatorId,
		current.Id,
		current.Title,
		current.Description,
		current.AssigneeId,
		uint(current.Priority),
	)
	if err != nil {
		t.Fatalf("UpdateTask unchanged failed: %v", err)
	}

	afterScore, err := g.Redis().ZScore(ctx, taskHotKey(current.TeamId), current.Id)
	if err != nil {
		t.Fatalf("read heat after update failed: %v", err)
	}

	afterCount, err := g.Redis().LLen(ctx, activityKey)
	if err != nil {
		t.Fatalf("read activity count after update failed: %v", err)
	}

	if afterScore != beforeScore {
		t.Fatalf("heat changed: before=%v after=%v", beforeScore, afterScore)
	}
	if afterCount != beforeCount {
		t.Fatalf("activity count changed: before=%v after=%v", beforeCount, afterCount)
	}
}
func TestUpdateTaskUpdatesAndClearsAssignee(t *testing.T) {
	ctx := gctx.New()
	original, operatorId, _ := updateTaskTestFixture(t)

	activityKey := fmt.Sprintf("team:activities:%d", original.TeamId)
	updatedTitle := original.Title + "-test"
	updatedDescription := original.Description + "-test"
	updatedPriority := uint(3)
	if original.Priority == 3 {
		updatedPriority = 2
	}

	err := UpdateTask(
		ctx,
		operatorId,
		original.Id,
		updatedTitle,
		updatedDescription,
		0,
		updatedPriority,
	)
	if err != nil {
		t.Fatalf("UpdateTask failed: %v", err)
	}
	t.Cleanup(func() {
		var originalAssignee any
		if original.AssigneeId > 0 {
			originalAssignee = original.AssigneeId
		}

		_, err := dao.Task.Ctx(ctx).Where("id", original.Id).Data(g.Map{
			"title":       original.Title,
			"description": original.Description,
			"assignee_id": originalAssignee,
			"priority":    original.Priority,
		}).Update()
		if err != nil {
			t.Errorf("restore task fields failed: %v", err)
		}

		if _, err := g.Redis().LPop(ctx, activityKey); err != nil {
			t.Errorf("remove test activity failed: %v", err)
		}

		if _, err := g.Redis().ZIncrBy(ctx, taskHotKey(original.TeamId), -1, original.Id); err != nil {
			t.Errorf("restore heat failed: %v", err)
		}
	})
	var updated entity.Task
	if err := dao.Task.Ctx(ctx).Where("id", original.Id).Scan(&updated); err != nil {
		t.Fatalf("query updated task failed: %v", err)
	}

	if updated.Title != updatedTitle ||
		updated.Description != updatedDescription ||
		updated.AssigneeId != 0 ||
		updated.Priority != int(updatedPriority) {
		t.Fatalf("unexpected updated task: %+v", updated)
	}

}

// updateTaskTestFixture finds one existing task, a member allowed to edit it,
// and a user outside its team. The permission tests remain valid as team data evolves.
func updateTaskTestFixture(t *testing.T) (entity.Task, uint64, uint64) {
	t.Helper()
	ctx := gctx.New()

	var task entity.Task
	if err := dao.Task.Ctx(ctx).OrderAsc("id").Limit(1).Scan(&task); err != nil {
		t.Fatalf("query task fixture failed: %v", err)
	}
	if task.Id == 0 {
		t.Fatal("task fixture does not exist")
	}

	var members []entity.TeamMember
	if err := dao.TeamMember.Ctx(ctx).Where("team_id", task.TeamId).Scan(&members); err != nil {
		t.Fatalf("query team members failed: %v", err)
	}
	if len(members) == 0 {
		t.Fatalf("team %d has no editable member fixture", task.TeamId)
	}

	memberIds := make(map[uint64]struct{}, len(members))
	for _, member := range members {
		memberIds[member.UserId] = struct{}{}
	}

	var users []entity.User
	if err := dao.User.Ctx(ctx).Scan(&users); err != nil {
		t.Fatalf("query user fixtures failed: %v", err)
	}
	for _, user := range users {
		if _, isMember := memberIds[user.Id]; !isMember {
			return task, members[0].UserId, user.Id
		}
	}

	t.Fatalf("team %d has no outside user fixture", task.TeamId)
	return entity.Task{}, 0, 0
}

func restoreTaskEditableFields(t *testing.T, original entity.Task) {
	t.Helper()
	ctx := gctx.New()

	var originalAssignee any
	if original.AssigneeId > 0 {
		originalAssignee = original.AssigneeId
	}

	_, err := dao.Task.Ctx(ctx).Where("id", original.Id).Data(g.Map{
		"title":       original.Title,
		"description": original.Description,
		"assignee_id": originalAssignee,
		"priority":    original.Priority,
	}).Update()
	if err != nil {
		t.Errorf("restore task editable fields failed: %v", err)
	}
}

func commentNotificationTestFixture(t *testing.T) (entity.Task, uint64, uint64) {
	t.Helper()

	task, commenterId, receiverId := statusNotificationTestFixture(t)
	return task, commenterId, receiverId
}

func cleanupCommentNotificationTest(t *testing.T, commentId uint64, notificationId uint64, receiverId uint64, activityKey string) {
	t.Helper()
	ctx := gctx.New()

	if commentId > 0 {
		if _, err := dao.TaskComment.Ctx(ctx).Where("id", commentId).Delete(); err != nil {
			t.Errorf("delete test comment failed: %v", err)
		}
	}
	if notificationId > 0 {
		if _, err := dao.Notification.Ctx(ctx).Where("id", notificationId).Delete(); err != nil {
			t.Errorf("delete test notification failed: %v", err)
		}
	}
	if receiverId > 0 && notificationId > 0 {
		if _, err := g.Redis().SRem(ctx, fmt.Sprintf("notification:unread:%d", receiverId), notificationId); err != nil {
			t.Errorf("remove notification from unread set failed: %v", err)
		}
	}
	if _, err := g.Redis().LPop(ctx, activityKey); err != nil {
		t.Errorf("remove test activity failed: %v", err)
	}
}

func TestUpdateStatusCreatesNotificationForAssignee(t *testing.T) {
	ctx := gctx.New()
	task, operatorId, assigneeId := statusNotificationTestFixture(t)
	newStatus := nextStatus(task.Status)
	activityKey := fmt.Sprintf("team:activities:%d", task.TeamId)

	_, err := dao.Task.Ctx(ctx).Where("id", task.Id).Data(g.Map{
		"assignee_id": assigneeId,
	}).Update()
	if err != nil {
		t.Fatalf("prepare assignee failed: %v", err)
	}

	err = UpdateStatus(ctx, operatorId, task.Id, newStatus)
	if err != nil {
		t.Fatalf("UpdateStatus failed: %v", err)
	}

	var created entity.Notification
	err = dao.Notification.Ctx(ctx).
		Where("receiver_id", assigneeId).
		Where("actor_id", operatorId).
		Where("type", notificationLogic.TypeTaskStatusUpdated).
		Where("related_task_id", task.Id).
		OrderDesc("id").
		Limit(1).
		Scan(&created)
	if err != nil {
		t.Fatalf("query created notification failed: %v", err)
	}
	if created.Id == 0 {
		t.Fatal("status update notification was not created")
	}
	if !strings.Contains(created.Content, newStatus) {
		t.Fatalf("notification content should include new status, got: %s", created.Content)
	}

	inUnreadSet, err := g.Redis().SIsMember(ctx, fmt.Sprintf("notification:unread:%d", assigneeId), created.Id)
	if err != nil {
		t.Fatalf("check unread set failed: %v", err)
	}
	if inUnreadSet != 1 {
		t.Fatalf("notification %d was not added to unread set", created.Id)
	}

	t.Cleanup(func() {
		restoreTaskForStatusTest(t, task)

		if _, err := dao.Notification.Ctx(ctx).Where("id", created.Id).Delete(); err != nil {
			t.Errorf("delete test notification failed: %v", err)
		}
		if _, err := g.Redis().SRem(ctx, fmt.Sprintf("notification:unread:%d", assigneeId), created.Id); err != nil {
			t.Errorf("remove test notification from unread set failed: %v", err)
		}
		if _, err := g.Redis().LPop(ctx, activityKey); err != nil {
			t.Errorf("remove test activity failed: %v", err)
		}
		if _, err := g.Redis().ZIncrBy(ctx, taskHotKey(task.TeamId), -1, task.Id); err != nil {
			t.Errorf("restore heat failed: %v", err)
		}
	})
}

func TestUpdateStatusUnchangedDoesNotCreateNotification(t *testing.T) {
	ctx := gctx.New()
	task, operatorId, assigneeId := statusNotificationTestFixture(t)
	activityKey := fmt.Sprintf("team:activities:%d", task.TeamId)

	beforeNotifications, err := dao.Notification.Ctx(ctx).
		Where("receiver_id", assigneeId).
		Where("actor_id", operatorId).
		Where("type", notificationLogic.TypeTaskStatusUpdated).
		Where("related_task_id", task.Id).
		Count()
	if err != nil {
		t.Fatalf("count notifications before update failed: %v", err)
	}

	beforeActivities, err := g.Redis().LLen(ctx, activityKey)
	if err != nil {
		t.Fatalf("read activity count before update failed: %v", err)
	}

	beforeHeat, err := g.Redis().ZScore(ctx, taskHotKey(task.TeamId), task.Id)
	if err != nil {
		t.Fatalf("read heat before update failed: %v", err)
	}

	err = UpdateStatus(ctx, operatorId, task.Id, task.Status)
	if err != nil {
		t.Fatalf("UpdateStatus unchanged failed: %v", err)
	}

	afterNotifications, err := dao.Notification.Ctx(ctx).
		Where("receiver_id", assigneeId).
		Where("actor_id", operatorId).
		Where("type", notificationLogic.TypeTaskStatusUpdated).
		Where("related_task_id", task.Id).
		Count()
	if err != nil {
		t.Fatalf("count notifications after update failed: %v", err)
	}

	afterActivities, err := g.Redis().LLen(ctx, activityKey)
	if err != nil {
		t.Fatalf("read activity count after update failed: %v", err)
	}

	afterHeat, err := g.Redis().ZScore(ctx, taskHotKey(task.TeamId), task.Id)
	if err != nil {
		t.Fatalf("read heat after update failed: %v", err)
	}

	if afterNotifications != beforeNotifications {
		t.Fatalf("notification count changed: before=%d after=%d", beforeNotifications, afterNotifications)
	}
	if afterActivities != beforeActivities {
		t.Fatalf("activity count changed: before=%d after=%d", beforeActivities, afterActivities)
	}
	if afterHeat != beforeHeat {
		t.Fatalf("heat changed: before=%v after=%v", beforeHeat, afterHeat)
	}
}

func TestUpdateStatusDoesNotNotifySelf(t *testing.T) {
	ctx := gctx.New()
	task, operatorId, _ := statusNotificationTestFixture(t)
	newStatus := nextStatus(task.Status)
	activityKey := fmt.Sprintf("team:activities:%d", task.TeamId)

	_, err := dao.Task.Ctx(ctx).Where("id", task.Id).Data(g.Map{
		"assignee_id": operatorId,
	}).Update()
	if err != nil {
		t.Fatalf("prepare self assignee failed: %v", err)
	}

	beforeNotifications, err := dao.Notification.Ctx(ctx).
		Where("receiver_id", operatorId).
		Where("actor_id", operatorId).
		Where("type", notificationLogic.TypeTaskStatusUpdated).
		Where("related_task_id", task.Id).
		Count()
	if err != nil {
		t.Fatalf("count notifications before update failed: %v", err)
	}

	err = UpdateStatus(ctx, operatorId, task.Id, newStatus)
	if err != nil {
		t.Fatalf("UpdateStatus self assignee failed: %v", err)
	}

	afterNotifications, err := dao.Notification.Ctx(ctx).
		Where("receiver_id", operatorId).
		Where("actor_id", operatorId).
		Where("type", notificationLogic.TypeTaskStatusUpdated).
		Where("related_task_id", task.Id).
		Count()
	if err != nil {
		t.Fatalf("count notifications after update failed: %v", err)
	}

	if afterNotifications != beforeNotifications {
		t.Fatalf("self notification should not be created: before=%d after=%d", beforeNotifications, afterNotifications)
	}

	t.Cleanup(func() {
		restoreTaskForStatusTest(t, task)

		if _, err := g.Redis().LPop(ctx, activityKey); err != nil {
			t.Errorf("remove test activity failed: %v", err)
		}
		if _, err := g.Redis().ZIncrBy(ctx, taskHotKey(task.TeamId), -1, task.Id); err != nil {
			t.Errorf("restore heat failed: %v", err)
		}
	})
}

func statusNotificationTestFixture(t *testing.T) (entity.Task, uint64, uint64) {
	t.Helper()
	ctx := gctx.New()

	var tasks []entity.Task
	if err := dao.Task.Ctx(ctx).OrderAsc("id").Scan(&tasks); err != nil {
		t.Fatalf("query task fixtures failed: %v", err)
	}

	for _, task := range tasks {
		var members []entity.TeamMember
		if err := dao.TeamMember.Ctx(ctx).Where("team_id", task.TeamId).OrderAsc("user_id").Scan(&members); err != nil {
			t.Fatalf("query team members failed: %v", err)
		}
		if len(members) >= 2 {
			return task, members[0].UserId, members[1].UserId
		}
	}

	t.Fatal("no task fixture with at least two team members")
	return entity.Task{}, 0, 0
}

func restoreTaskForStatusTest(t *testing.T, original entity.Task) {
	t.Helper()
	ctx := gctx.New()

	var originalAssignee any
	if original.AssigneeId > 0 {
		originalAssignee = original.AssigneeId
	}

	_, err := dao.Task.Ctx(ctx).Where("id", original.Id).Data(g.Map{
		"status":      original.Status,
		"assignee_id": originalAssignee,
	}).Update()
	if err != nil {
		t.Errorf("restore task status fields failed: %v", err)
	}
}

func nextStatus(status string) string {
	if status == "doing" {
		return "done"
	}
	return "doing"
}
