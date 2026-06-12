package task

import (
	"context"
	lockLogic "redis-demo/internal/logic/lock"
	"strings"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// === 常量 ===
// 1. 衰减系数：heatDecayFactor = 0.9
// 2. 衰减间隔：heatDecayInterval = 1 小时
// 3. 分布式锁 key 和 TTL
const (
	heatDecayFactor         = 0.9           // 每次保留 90%
	heatDecayInterval       = 1 * time.Hour // 每小时衰减一次
	heatDecayScannerLockKey = "lock:task:heat:decay"
	heatDecayScannerLockTTL = 50 * time.Second
)

// === Lua 脚本 ===
// 1. KEYS[1] = 排行榜 key，ARGV[1] = 衰减系数
// 2. ZRANGE 读取所有成员和分数
// 3. 遍历，每个分数 × factor
// 4. ZADD 写回新分数
// 5. 返回被衰减的任务数量
var heatDecayLuaScript = `
local key = KEYS[1]
local factor = tonumber(ARGV[1])
local members = redis.call('ZRANGE', key, 0, -1, 'WITHSCORES')
local count = 0
for i = 1, #members, 2 do
    local member = members[i]
    local score = tonumber(members[i + 1])
    local newScore = score * factor
    redis.call('ZADD', key, newScore, member)
    count = count + 1
end
return count
`

// === 函数 1：DecayTeamHotScore ===
// 对单个团队总榜执行衰减
// 1. 生成总榜 key
// 2. EVAL 执行 Lua 脚本
// 3. 返回被衰减数量
func DecayTeamHotScore(ctx context.Context, teamId uint64, factor float64) (int64, error) {
	key := taskHotKey(teamId)
	result, err := g.Redis().Do(ctx, "EVAL", heatDecayLuaScript, 1, key, factor)
	if err != nil {
		return 0, err
	}
	return result.Int64(), nil
}

// === 函数 2：scanHotTaskKeys ===
// SCAN 扫描所有团队总榜 key
// 1. MATCH 模式 team:task:hot:*
// 2. 循环 SCAN 直到 cursor 为 0
// 3. 过滤掉包含 daily/weekly 的 key
func scanHotTaskKeys(ctx context.Context) ([]string, error) {
	pattern := "team:task:hot:*" //匹配模式
	var cursor uint64            //游标,从0开始
	var keys []string
	for {
		// ③ SCAN cursor MATCH pattern COUNT 100
		result, err := g.Redis().Do(ctx, "SCAN", cursor, "MATCH", pattern, "COUNT", 100) //SCAN 命令,
		if err != nil {
			return nil, err
		}
		parts := result.Vars() // ④ [cursor, [key1, key2, ...]]
		if len(parts) != 2 {
			break
		}

		cursor = parts[0].Uint64() // ⑤ 下一次扫描的游标
		for _, item := range parts[1].Vars() {
			key := item.String()
			// ⑥ 过滤掉日榜和周榜
			if !strings.Contains(key, "daily") && !strings.Contains(key, "weekly") {
				keys = append(keys, key)
			}
		}
		if cursor == 0 { // ⑦ cursor 回到 0 表示扫描完成
			break
		}
	}
	return keys, nil
}

// === 函数 3：DecayAllTeamHotScores ===
// 遍历所有团队总榜并衰减
// 1. 调用 scanHotTaskKeys 获取所有 key
// 2. 逐个执行 Lua 脚本衰减
// 3. 单个 key 失败只记日志，不中断
func DecayAllTeamHotScores(ctx context.Context, factor float64) error {
	keys, err := scanHotTaskKeys(ctx)
	if err != nil {
		g.Log().Error(ctx, "scan hot task keys error", err)
		return err
	}
	for _, key := range keys {
		result, err := g.Redis().Do(ctx, "EVAL", heatDecayLuaScript, 1, key, factor)
		if err != nil {
			// ③ 某个 key 失败只记日志，继续处理下一个
			g.Log().Errorf(ctx, "heat decay failed for key %s:%v", key, err)
			continue
		}
		count := result.Int64()
		if count > 0 {
			g.Log().Infof(ctx, "heat decay for key %s:%d", key, count)
		}
	}
	return nil
}

// === 函数 4：StartHeatDecayWorker ===
// 后台 worker，定时触发
// 1. goroutine + ticker
// 2. 监听 ctx.Done 退出
// 3. ticker 触发时调用带锁版本
func StartHeatDecayWorker(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(heatDecayInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := decayAllTeamHotScoresWithLock(ctx); err != nil {
					g.Log().Error(ctx, "heat decay worker error", "err:", err)
				}
			}
		}
	}()
}

// === 函数 5：decayAllTeamHotScoresWithLock ===
// 带分布式锁的衰减
// 1. TryLock
// 2. 没拿到锁则跳过
// 3. defer Unlock
// 4. 调用 DecayAllTeamHotScores
func decayAllTeamHotScoresWithLock(ctx context.Context) error {
	// ④ 尝试拿锁
	lock, locked, err := lockLogic.TryLock(ctx, heatDecayScannerLockKey, heatDecayScannerLockTTL)
	if err != nil {
		return err
	}
	// ⑤ 没拿到锁 → 其他实例正在执行，跳过
	if !locked {
		return nil
	}
	// ⑥ 拿到锁 → 确保最终释放
	defer func() {
		if err := lockLogic.Unlock(ctx, lock); err != nil {
			g.Log().Error(ctx, "unlock heat decay scanner failed", "err", err)

		}
	}()
	// ⑦ 执行衰减
	return DecayAllTeamHotScores(ctx, heatDecayFactor)
}
