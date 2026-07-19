package selector

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/babutree/GeoProxy/affinity"
	"github.com/babutree/GeoProxy/auth"
	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/storage"
)

const unknownLatencyRank = 1 << 30

const (
	// sessionLatencyWeightScale 控制延迟优势的衰减尺度。
	sessionLatencyWeightScale = 250.0
	// reliabilityPriorSuccess/reliabilityPriorTotal 构成 Beta(2,2) 先验，
	// 避免零样本节点被当作完美或完全不可靠。
	reliabilityPriorSuccess = 2.0
	reliabilityPriorTotal   = 4.0
	reliabilityWeightFloor  = 0.75
	reliabilityWeightRange  = 0.5
	loadWeightFloor         = 0.5
)

var ErrNoNode = errors.New("no available node")

// afterFirstBindPickHook 是仅测试使用的切入点：设置后，Resolve 在
// pickForSession 返回候选、写入绑定之前调用它；生产环境保持 nil。
var afterFirstBindPickHook func()

type Store interface {
	GetByRegion(region string, excludes []int64) ([]storage.Proxy, error)
	GetProxyByID(id int64) (*storage.Proxy, error)
	// GetProxyByAddress 按拨号地址（host:port）查询单个节点。
	// 用于兼容旧 -node-host:port；地址不唯一/不存在时返回 error。
	GetProxyByAddress(address string) (*storage.Proxy, error)
	// GetProxyByNodeKey 按稳定配置身份查询（-node-key-…）。
	GetProxyByNodeKey(nodeKey string) (*storage.Proxy, error)
	// IsSubscriptionPaused 报告父订阅是否暂停；id<=0（手工节点）恒为 false。
	IsSubscriptionPaused(id int64) (bool, error)
}

func Pick(store Store, region string, excludes []int64) (*storage.Proxy, error) {
	return PickUnlock(store, region, excludes, nil)
}

// PickUnlock 在地域选路基础上叠加 unlock 过滤（openai/claude/grok/gemini/cf）。
// unlock 为空时与 Pick 完全一致。
func PickUnlock(store Store, region string, excludes []int64, unlock []string) (*storage.Proxy, error) {
	region = normalizeRegion(region)
	proxies, err := store.GetByRegion(region, excludes)
	if err != nil {
		return nil, err
	}
	available := availableProxies(proxies)
	available = filterByUnlock(available, unlock)
	if len(available) == 0 {
		return nil, noNodeError(region)
	}
	return pickLowestLatency(available), nil
}

func Resolve(store Store, sessions *affinity.Store, route auth.ParsedUsername, excludes []int64) (*storage.Proxy, error) {
	// -node- 锁定优先：直接命中指定入口节点，绕过地域选路与会话亲和。
	// 仍须通过可用性/地域/unlock/父订阅校验；被 excludes 命中或校验失败则返回 ErrNoNode。
	// 注意：锁定的是网关拨号的入口地址（节点身份），最终出口 IP 由该节点上游链路决定，
	// 链式/realm 转发时可能与入口不同或漂移，网关无法感知或保证。
	if route.Node != "" {
		return resolvePinnedNode(store, route, excludes)
	}
	if route.Session == "" {
		return PickUnlock(store, route.Region, excludes, route.Unlock)
	}
	// sticky 快速路径：不获取首次绑定锁（仅读取并刷新）。
	if proxy, ok := stickyBoundProxy(store, sessions, route, excludes); ok {
		return proxy, nil
	}
	if sessions == nil {
		return pickForSession(store, nil, route.Region, route.Session, excludes, maxSessionsPerProxy(), proxyCooldownMinutes(), route.Unlock)
	}

	// 首次绑定/重新绑定：串行执行检查、释放占用、选节点和写入。
	sessions.BeginFirstBind()
	defer sessions.EndFirstBind()

	if proxy, ok := stickyBoundProxy(store, sessions, route, excludes); ok {
		return proxy, nil
	}
	rebindRegion := releaseStaleBinding(sessions, route, excludes)
	maxSessions := maxSessionsPerProxy()
	cooldownMinutes := proxyCooldownMinutes()
	proxy, err := pickForSession(store, sessions, rebindRegion, route.Session, excludes, maxSessions, cooldownMinutes, route.Unlock)
	if err != nil {
		return nil, err
	}
	if afterFirstBindPickHook != nil {
		afterFirstBindPickHook()
	}
	sessions.SetProxy(route.Session, proxy.ID, proxy.Address, proxy.Region)
	if cooldownMinutes > 0 {
		sessions.SetCooldown(proxy.ID, sessionsNow(sessions).Add(time.Duration(cooldownMinutes)*time.Minute))
	}
	return proxy, nil
}

// resolvePinnedNode 按 -node- 令牌命中单一节点。
// 支持 key-<nodeKey>（稳定身份）与 host:port（兼容旧复制）。
// 校验顺序：excludes → 存在 → 可用 → 地域匹配 → unlock 过滤 → 父订阅未暂停。
// 任一不满足返回 ErrNoNode（不回退到普通选路，锁定语义必须显式失败）。
func resolvePinnedNode(store Store, route auth.ParsedUsername, excludes []int64) (*storage.Proxy, error) {
	var (
		proxy *storage.Proxy
		err   error
	)
	if strings.HasPrefix(route.Node, "key-") {
		proxy, err = store.GetProxyByNodeKey(strings.TrimPrefix(route.Node, "key-"))
	} else {
		proxy, err = store.GetProxyByAddress(route.Node)
	}
	if err != nil || proxy == nil {
		return nil, noNodeError(route.Region)
	}
	if excluded(proxy.ID, excludes) {
		return nil, noNodeError(route.Region)
	}
	if !proxyAvailable(*proxy) {
		return nil, noNodeError(route.Region)
	}
	if regionMismatch(proxy.Region, route.Region) {
		return nil, noNodeError(route.Region)
	}
	if !proxyMatchesUnlock(*proxy, route.Unlock) {
		return nil, noNodeError(route.Region)
	}
	if proxy.SubscriptionID > 0 {
		paused, err := store.IsSubscriptionPaused(proxy.SubscriptionID)
		if err != nil || paused {
			return nil, noNodeError(route.Region)
		}
	}
	return proxy, nil
}

// pickForSession 为一个 session 首次绑定选节点：对所有通过可用性、
// unlock、occupancy、cooldown 与 excludes 过滤的候选做稳定加权 rendezvous
// hashing。同一 session、同一候选状态恒定映射同一节点，输入顺序不影响结果；
// 延迟、历史可靠性与活跃绑定负载仅作为有界软权重，不会永久排除合格节点。
// maxSessions > 0 时过滤 occupancy >= max 的节点；0 表示不限制。
// cooldownMinutes > 0 时过滤 InCooldown 节点；0 表示忽略冷却表（读侧关闭）。
func pickForSession(store Store, sessions *affinity.Store, region, session string, excludes []int64, maxSessions, cooldownMinutes int, unlock []string) (*storage.Proxy, error) {
	region = normalizeRegion(region)
	proxies, err := store.GetByRegion(region, excludes)
	if err != nil {
		return nil, err
	}
	available := availableProxies(proxies)
	available = filterByUnlock(available, unlock)
	if len(available) == 0 {
		return nil, noNodeError(region)
	}
	if maxSessions > 0 && sessions != nil {
		filtered := filterByOccupancy(available, sessions, maxSessions)
		if len(filtered) == 0 {
			return nil, noNodeCapacityError(region)
		}
		available = filtered
	}
	if cooldownMinutes > 0 && sessions != nil {
		filtered := filterByCooldown(available, sessions)
		if len(filtered) == 0 {
			return nil, noNodeCooldownError(region)
		}
		available = filtered
	}
	picked := pickSessionCandidateWithAffinity(available, session, sessions)
	return &picked, nil
}

// pickSessionCandidate 使用 weighted rendezvous hashing 在全部候选中选出
// 分数最小者；nil affinity 表示没有可观测的活跃绑定负载。
func pickSessionCandidate(proxies []storage.Proxy, session string) storage.Proxy {
	return pickSessionCandidateWithAffinity(proxies, session, nil)
}

// pickSessionCandidateWithAffinity 保持哈希身份稳定，同时读取当前活跃绑定数。
// LastCheck/LastUsed 是时间元数据，QualityGrade 又由延迟派生，均不重复计权。
func pickSessionCandidateWithAffinity(proxies []storage.Proxy, session string, sessions *affinity.Store) storage.Proxy {
	activeBindings := activeBindingCounts(sessions)
	sessionSeed := sessionHashSeed(session)
	picked := proxies[0]
	bestScore := sessionRendezvousScoreWithSeed(sessionSeed, picked, activeBindings[picked.ID])
	bestTieKey := ""
	hasBestTieKey := false
	for _, proxy := range proxies[1:] {
		score := sessionRendezvousScoreWithSeed(sessionSeed, proxy, activeBindings[proxy.ID])
		if score < bestScore {
			picked = proxy
			bestScore = score
			hasBestTieKey = false
			continue
		}
		if score == bestScore {
			if !hasBestTieKey {
				bestTieKey = sessionProxyTieKey(picked)
				hasBestTieKey = true
			}
			tieKey := sessionProxyTieKey(proxy)
			if tieKey < bestTieKey {
				picked = proxy
				bestTieKey = tieKey
			}
		}
	}
	return picked
}

// sessionRendezvousScore 将稳定的 [0,1] 哈希样本转换为指数竞赛分数。
// 保留无负载参数的测试辅助入口，等价于零活跃绑定。
func sessionRendezvousScore(session string, proxy storage.Proxy) float64 {
	return sessionRendezvousScoreWithLoad(session, proxy, 0)
}

// sessionRendezvousScoreWithLoad 分数越小越优；用 53 位样本避免整数到
// 浮点转换造成平台相关的边界。
func sessionRendezvousScoreWithLoad(session string, proxy storage.Proxy, activeBindings int) float64 {
	return sessionRendezvousScoreWithSeed(sessionHashSeed(session), proxy, activeBindings)
}

func sessionRendezvousScoreWithSeed(sessionSeed uint64, proxy storage.Proxy, activeBindings int) float64 {
	raw := mixSessionHash(sessionProxyHash(sessionSeed, proxy))
	const mantissaRange = uint64(1) << 53
	u := float64((raw>>11)+1) / float64(mantissaRange)
	return -math.Log(u) / sessionCandidateWeight(proxy, activeBindings)
}

const (
	sessionFNV64Offset = uint64(14695981039346656037)
	sessionFNV64Prime  = uint64(1099511628211)
)

func sessionHashSeed(session string) uint64 {
	return sessionFNVWriteByte(sessionFNVWriteString(sessionFNV64Offset, session), 0)
}

// sessionProxyHash 保持原有文本命名空间的字节序，但直接写入 FNV 状态，
// 避免每个候选创建 identity 字符串和 []byte 临时对象。
func sessionProxyHash(seed uint64, proxy storage.Proxy) uint64 {
	switch {
	case proxy.ID != 0:
		seed = sessionFNVWriteString(seed, "id:")
		return sessionFNVWriteInt64(seed, proxy.ID)
	case proxy.NodeKey != "":
		seed = sessionFNVWriteString(seed, "key:")
		return sessionFNVWriteString(seed, proxy.NodeKey)
	default:
		seed = sessionFNVWriteString(seed, "address:")
		return sessionFNVWriteString(seed, proxy.Address)
	}
}

func sessionFNVWriteString(hash uint64, value string) uint64 {
	for i := 0; i < len(value); i++ {
		hash = sessionFNVWriteByte(hash, value[i])
	}
	return hash
}

func sessionFNVWriteByte(hash uint64, value byte) uint64 {
	return (hash ^ uint64(value)) * sessionFNV64Prime
}

func sessionFNVWriteInt64(hash uint64, value int64) uint64 {
	var magnitude uint64
	if value < 0 {
		hash = sessionFNVWriteByte(hash, '-')
		magnitude = uint64(-(value + 1)) + 1
	} else {
		magnitude = uint64(value)
	}

	var digits [20]byte
	index := len(digits)
	if magnitude == 0 {
		index--
		digits[index] = '0'
	}
	for magnitude > 0 {
		index--
		digits[index] = byte('0' + magnitude%10)
		magnitude /= 10
	}
	for ; index < len(digits); index++ {
		hash = sessionFNVWriteByte(hash, digits[index])
	}
	return hash
}

// mixSessionHash 对 FNV 输出做 avalanche，消除相似短节点身份之间的后缀相关性。
func mixSessionHash(value uint64) uint64 {
	value ^= value >> 30
	value *= 0xbf58476d1ce4e5b9
	value ^= value >> 27
	value *= 0x94d049bb133111eb
	value ^= value >> 31
	return value
}

// sessionCandidateWeight 将三个正值有界因子相乘，总范围约为 [0.375,2.5)。
func sessionCandidateWeight(proxy storage.Proxy, activeBindings int) float64 {
	return sessionLatencyWeight(proxy.Latency) *
		sessionReliabilityWeight(proxy) *
		sessionLoadWeight(activeBindings)
}

// sessionLatencyWeight 把正延迟映射到 (1,2) 的温和权重；未知延迟为 1。
func sessionLatencyWeight(latency int) float64 {
	if latency <= 0 {
		return 1
	}
	return 1 + sessionLatencyWeightScale/(sessionLatencyWeightScale+float64(latency))
}

// sessionReliabilityWeight 使用累计成功/使用次数的 Beta(2,2) 平滑估计，
// 再映射到 (0.75,1.25)。FailCount 已作为可用性硬门槛，不重复计权。
func sessionReliabilityWeight(proxy storage.Proxy) float64 {
	uses := proxy.UseCount
	if uses < 0 {
		uses = 0
	}
	successes := proxy.SuccessCount
	if successes < 0 {
		successes = 0
	}
	if successes > uses {
		successes = uses
	}
	rate := (float64(successes) + reliabilityPriorSuccess) /
		(float64(uses) + reliabilityPriorTotal)
	return reliabilityWeightFloor + reliabilityWeightRange*rate
}

// sessionLoadWeight 根据当前活跃绑定数降权，范围为 (0.5,1]。
// 负载再高也保留探索下限；负值按零处理以容忍异常测试输入。
func sessionLoadWeight(activeBindings int) float64 {
	if activeBindings < 0 {
		activeBindings = 0
	}
	return loadWeightFloor + (1-loadWeightFloor)/(1+float64(activeBindings))
}

// activeBindingCounts 通过一次只读快照统计负载，避免按候选重复加锁。
// ProxyID<=0 无法归属到稳定数据库节点，因此不参与负载计数。
func activeBindingCounts(sessions *affinity.Store) map[int64]int {
	if sessions == nil {
		return nil
	}
	bindings := sessions.List()
	if len(bindings) == 0 {
		return nil
	}
	counts := make(map[int64]int, len(bindings))
	for _, binding := range bindings {
		if binding.ProxyID > 0 {
			counts[binding.ProxyID]++
		}
	}
	return counts
}

// sessionProxyIdentity 优先使用稳定数据库 ID；旧数据缺 ID 时退回 NodeKey，
// 再退回地址。地址可能因隧道端口重分配而变化，因此不应覆盖非零 ID。
func sessionProxyIdentity(proxy storage.Proxy) string {
	if proxy.ID != 0 {
		return fmt.Sprintf("id:%d", proxy.ID)
	}
	if proxy.NodeKey != "" {
		return "key:" + proxy.NodeKey
	}
	return "address:" + proxy.Address
}

// sessionProxyTieKey 为极低概率的同分数场景提供输入顺序无关的确定性决胜。
func sessionProxyTieKey(proxy storage.Proxy) string {
	return fmt.Sprintf("%s\\x00%s\\x00%s\\x00%d", sessionProxyIdentity(proxy), proxy.NodeKey, proxy.Address, proxy.ID)
}

func filterByOccupancy(proxies []storage.Proxy, sessions *affinity.Store, maxSessions int) []storage.Proxy {
	if maxSessions <= 0 || sessions == nil {
		return proxies
	}
	out := make([]storage.Proxy, 0, len(proxies))
	for _, proxy := range proxies {
		if sessions.CountByProxy(proxy.ID) < maxSessions {
			out = append(out, proxy)
		}
	}
	return out
}

func filterByCooldown(proxies []storage.Proxy, sessions *affinity.Store) []storage.Proxy {
	if sessions == nil {
		return proxies
	}
	out := make([]storage.Proxy, 0, len(proxies))
	for _, proxy := range proxies {
		if !sessions.InCooldown(proxy.ID) {
			out = append(out, proxy)
		}
	}
	return out
}

func maxSessionsPerProxy() int {
	cfg := config.Get()
	if cfg == nil {
		return 0
	}
	if cfg.MaxSessionsPerProxy < 0 {
		return 0
	}
	return cfg.MaxSessionsPerProxy
}

func proxyCooldownMinutes() int {
	cfg := config.Get()
	if cfg == nil {
		return 0
	}
	if cfg.ProxyCooldownMinutes < 0 {
		return 0
	}
	return cfg.ProxyCooldownMinutes
}

// sessionsNow 返回 affinity store 的时钟，使 cooldown_until 与
// 可注入测试时钟下的 InCooldown 保持一致。
func sessionsNow(sessions *affinity.Store) time.Time {
	if sessions == nil {
		return time.Now()
	}
	return sessions.Now()
}

// stickyBoundProxy 返回仍存活且地域匹配的绑定，不修改占用。
// 需要重新绑定的调用方必须在首次绑定锁内使用 releaseStaleBinding。
func stickyBoundProxy(store Store, sessions *affinity.Store, route auth.ParsedUsername, excludes []int64) (*storage.Proxy, bool) {
	if sessions == nil {
		return nil, false
	}
	binding, ok := sessions.Get(route.Session)
	if !ok {
		return nil, false
	}
	if binding.ProxyID <= 0 || excluded(binding.ProxyID, excludes) || bindingRegionMismatch(binding, route.Region) {
		return nil, false
	}
	proxy, err := store.GetProxyByID(binding.ProxyID)
	if err != nil || proxy == nil || !proxyAvailable(*proxy) || regionMismatch(proxy.Region, route.Region) {
		return nil, false
	}
	// sticky 绑定也必须满足当前 unlock 过滤，否则释放并重新选路。
	if !proxyMatchesUnlock(*proxy, route.Unlock) {
		return nil, false
	}
	// 父订阅暂停后节点整体不得参与选路；sticky 不得绕过该契约。
	if proxy.SubscriptionID > 0 {
		paused, err := store.IsSubscriptionPaused(proxy.SubscriptionID)
		if err != nil || paused {
			return nil, false
		}
	}
	return proxy, true
}

// releaseStaleBinding 删除非 sticky 绑定，避免其占用阻塞重新绑定。
// 必须在首次绑定串行区内调用。
func releaseStaleBinding(sessions *affinity.Store, route auth.ParsedUsername, excludes []int64) string {
	binding, ok := sessions.Get(route.Session)
	if !ok {
		return route.Region
	}
	rebindRegion := requestedOrBoundRegion(route.Region, binding.Region)
	if binding.ProxyID <= 0 || excluded(binding.ProxyID, excludes) || bindingRegionMismatch(binding, route.Region) {
		sessions.Remove(route.Session)
		return rebindRegion
	}
	// 绑定存在，但 stickyBoundProxy 已拒绝它（节点失效或地域不符）。
	sessions.Remove(route.Session)
	return rebindRegion
}

func availableProxies(proxies []storage.Proxy) []storage.Proxy {
	available := make([]storage.Proxy, 0, len(proxies))
	for _, proxy := range proxies {
		if proxyAvailable(proxy) {
			available = append(available, proxy)
		}
	}
	return available
}

func pickLowestLatency(proxies []storage.Proxy) *storage.Proxy {
	bestRank := latencyRank(proxies[0].Latency)
	candidates := []storage.Proxy{proxies[0]}
	for _, proxy := range proxies[1:] {
		rank := latencyRank(proxy.Latency)
		if rank < bestRank {
			bestRank = rank
			candidates = []storage.Proxy{proxy}
			continue
		}
		if rank == bestRank {
			candidates = append(candidates, proxy)
		}
	}
	picked := candidates[rand.Intn(len(candidates))]
	return &picked
}

func proxyAvailable(proxy storage.Proxy) bool {
	return !proxy.UserPaused && (proxy.Status == "active" || proxy.Status == "degraded") && proxy.FailCount < 3
}

func latencyRank(latency int) int {
	if latency <= 0 {
		return unknownLatencyRank
	}
	return latency
}

func excluded(proxyID int64, excludes []int64) bool {
	for _, exclude := range excludes {
		if exclude == proxyID {
			return true
		}
	}
	return false
}

func bindingRegionMismatch(binding affinity.Binding, region string) bool {
	return regionMismatch(binding.Region, region)
}

func requestedOrBoundRegion(requestedRegion string, boundRegion string) string {
	if requestedRegion != "" {
		return requestedRegion
	}
	return boundRegion
}

func regionMismatch(nodeRegion string, requestedRegion string) bool {
	return requestedRegion != "" && normalizeRegion(nodeRegion) != normalizeRegion(requestedRegion)
}

func normalizeRegion(region string) string {
	return strings.ToLower(strings.TrimSpace(region))
}

func noNodeError(region string) error {
	if region == "" {
		return ErrNoNode
	}
	return fmt.Errorf("%w for region: %s", ErrNoNode, region)
}

func noNodeCapacityError(region string) error {
	if region == "" {
		return fmt.Errorf("%w (capacity)", ErrNoNode)
	}
	return fmt.Errorf("%w for region: %s (capacity)", ErrNoNode, region)
}

func noNodeCooldownError(region string) error {
	if region == "" {
		return fmt.Errorf("%w (cooldown)", ErrNoNode)
	}
	return fmt.Errorf("%w for region: %s (cooldown)", ErrNoNode, region)
}
