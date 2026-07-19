package affinity

import (
	"sync"
	"time"
)

type Binding struct {
	ProxyID     int64
	NodeAddress string
	Region      string
	LastActive  time.Time
}

type Store struct {
	mu       sync.RWMutex
	bindings map[string]Binding
	// reverse 将 proxy_id 映射到当前绑定且未过期的 session_id 集合。
	// SetProxy/Remove/Get 过期处理/GC 会同步维护该索引。
	reverse map[int64]map[string]struct{}
	// cooldown 将 proxy_id 映射到 cooldown_until（不包含结束时刻）。
	// 新会话首次绑定时写入；粘性命中不会查询该映射。
	cooldown map[int64]time.Time
	ttl      time.Duration
	now      func() time.Time

	// firstBindMu 串行化首次绑定/重新绑定的检查后写入流程，
	// 防止并发会话的容量与冷却决策相互穿插。
	firstBindMu sync.Mutex

	// GC 生命周期字段，由 mu 保护。
	gcStarted bool
	stopCh    chan struct{}
	doneCh    chan struct{}
}

// BeginFirstBind 锁定首次绑定临界区；完成后必须调用 EndFirstBind。
// 粘性 Get 路径不得持有此锁。
func (s *Store) BeginFirstBind() {
	s.firstBindMu.Lock()
}

// EndFirstBind 释放首次绑定临界区。
func (s *Store) EndFirstBind() {
	s.firstBindMu.Unlock()
}

// SessionBinding 是单个活动会话绑定的只读快照，
// 可供 WebUI 会话监控面板展示。
type SessionBinding struct {
	SessionID   string
	ProxyID     int64
	NodeAddress string
	Region      string
	LastActive  time.Time
}

func New(ttl time.Duration) *Store {
	return NewWithClock(ttl, time.Now)
}

func NewWithClock(ttl time.Duration, now func() time.Time) *Store {
	return &Store{
		bindings: map[string]Binding{},
		reverse:  map[int64]map[string]struct{}{},
		cooldown: map[int64]time.Time{},
		ttl:      ttl,
		now:      now,
	}
}

func (s *Store) Get(sessionID string) (Binding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	binding, ok := s.bindings[sessionID]
	if !ok {
		return Binding{}, false
	}
	if s.expired(binding) {
		s.removeBindingLocked(sessionID, binding)
		return Binding{}, false
	}
	binding.LastActive = s.now()
	s.bindings[sessionID] = binding
	return binding, true
}

func (s *Store) Set(sessionID string, nodeAddress string, region string) {
	s.SetProxy(sessionID, 0, nodeAddress, region)
}

func (s *Store) SetProxy(sessionID string, proxyID int64, nodeAddress string, region string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.bindings[sessionID]; ok {
		s.detachReverseLocked(sessionID, old.ProxyID)
	}
	s.bindings[sessionID] = Binding{ProxyID: proxyID, NodeAddress: nodeAddress, Region: region, LastActive: s.now()}
	s.attachReverseLocked(sessionID, proxyID)
}

func (s *Store) Remove(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if binding, ok := s.bindings[sessionID]; ok {
		s.removeBindingLocked(sessionID, binding)
	}
}

// RemoveIfProxyID 仅在会话仍绑定到 expectedProxyID 时删除，避免旧失败请求
// 删除已被并发请求重新绑定到健康节点的会话。
func (s *Store) RemoveIfProxyID(sessionID string, expectedProxyID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	binding, ok := s.bindings[sessionID]
	if !ok {
		return false
	}
	if s.expired(binding) {
		s.removeBindingLocked(sessionID, binding)
		return false
	}
	if binding.ProxyID != expectedProxyID {
		return false
	}
	s.removeBindingLocked(sessionID, binding)
	return true
}

// SetCooldown 记录 proxyID 在指定绝对时刻之前不得接收新的首次绑定。
// 粘性会话不受影响。需要禁用 CD 的调用方应直接跳过调用
// （或在选择器读取路径配置 CD=0）。
func (s *Store) SetCooldown(proxyID int64, until time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cooldown == nil {
		s.cooldown = map[int64]time.Time{}
	}
	s.cooldown[proxyID] = until
}

// InCooldown 报告 proxyID 是否仍处于记录的冷却窗口（now < until）；
// 过期条目会被惰性清理。
func (s *Store) InCooldown(proxyID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	until, ok := s.cooldown[proxyID]
	if !ok {
		return false
	}
	if !s.now().Before(until) {
		delete(s.cooldown, proxyID)
		return false
	}
	return true
}

// CooldownRemaining 返回 proxyID 离开冷却期前的剩余时间；已离开则返回 0。
func (s *Store) CooldownRemaining(proxyID int64) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	until, ok := s.cooldown[proxyID]
	if !ok {
		return 0
	}
	rem := until.Sub(s.now())
	if rem <= 0 {
		delete(s.cooldown, proxyID)
		return 0
	}
	return rem
}

// CountByProxy 返回当前绑定到 proxyID 的未过期会话数。
// 它会清理正向绑定缺失或已过期的反向索引条目。
func (s *Store) CountByProxy(proxyID int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.countByProxyLocked(proxyID)
}

func (s *Store) countByProxyLocked(proxyID int64) int {
	sessions, ok := s.reverse[proxyID]
	if !ok || len(sessions) == 0 {
		return 0
	}
	for sessionID := range sessions {
		binding, ok := s.bindings[sessionID]
		if !ok || binding.ProxyID != proxyID {
			delete(sessions, sessionID)
			continue
		}
		if s.expired(binding) {
			s.removeBindingLocked(sessionID, binding)
			continue
		}
	}
	if len(sessions) == 0 {
		delete(s.reverse, proxyID)
		return 0
	}
	return len(sessions)
}

func (s *Store) attachReverseLocked(sessionID string, proxyID int64) {
	set, ok := s.reverse[proxyID]
	if !ok {
		set = map[string]struct{}{}
		s.reverse[proxyID] = set
	}
	set[sessionID] = struct{}{}
}

func (s *Store) detachReverseLocked(sessionID string, proxyID int64) {
	set, ok := s.reverse[proxyID]
	if !ok {
		return
	}
	delete(set, sessionID)
	if len(set) == 0 {
		delete(s.reverse, proxyID)
	}
}

func (s *Store) removeBindingLocked(sessionID string, binding Binding) {
	delete(s.bindings, sessionID)
	s.detachReverseLocked(sessionID, binding.ProxyID)
}

func (s *Store) expired(binding Binding) bool {
	return s.ttl > 0 && s.now().Sub(binding.LastActive) >= s.ttl
}

// StartGC 启动后台 goroutine，按 interval 扫描并删除过期绑定。
// GC goroutine 已运行时会忽略后续调用（需先调用 Stop 才能重启）；
// interval <= 0 时不执行任何操作。
func (s *Store) StartGC(interval time.Duration) {
	if interval <= 0 {
		return
	}

	s.mu.Lock()
	if s.gcStarted {
		s.mu.Unlock()
		return
	}
	s.gcStarted = true
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	s.stopCh = stopCh
	s.doneCh = doneCh
	s.mu.Unlock()

	go s.gcLoop(interval, stopCh, doneCh)
}

func (s *Store) gcLoop(interval time.Duration, stopCh, doneCh chan struct{}) {
	defer close(doneCh)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			s.collectExpired()
		}
	}
}

// collectExpired 在一次加锁过程中删除所有过期绑定，
// 并清理超过截止时刻的冷却条目。冷却清理不依赖代理再次被查询，
// 因此已永久离开池的代理条目也能回收，避免泄漏。
func (s *Store) collectExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for sessionID, binding := range s.bindings {
		if s.expired(binding) {
			s.removeBindingLocked(sessionID, binding)
		}
	}

	now := s.now()
	for proxyID, until := range s.cooldown {
		if !now.Before(until) {
			delete(s.cooldown, proxyID)
		}
	}
}

// Stop 平稳停止 GC goroutine。即使从未调用 StartGC 或此前已调用 Stop，
// 再次调用也安全；既不会 panic，也不会泄漏 goroutine。
func (s *Store) Stop() {
	s.mu.Lock()
	if !s.gcStarted {
		s.mu.Unlock()
		return
	}
	s.gcStarted = false
	stopCh := s.stopCh
	doneCh := s.doneCh
	s.stopCh = nil
	s.doneCh = nil
	s.mu.Unlock()

	close(stopCh)
	<-doneCh
}

// List 返回所有活动（未过期）绑定的快照。该操作只读：
// 不会刷新 LastActive，也不会删除过期条目；过期绑定会被跳过。
func (s *Store) List() []SessionBinding {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]SessionBinding, 0, len(s.bindings))
	for sessionID, binding := range s.bindings {
		if s.expired(binding) {
			continue
		}
		result = append(result, SessionBinding{
			SessionID:   sessionID,
			ProxyID:     binding.ProxyID,
			NodeAddress: binding.NodeAddress,
			Region:      binding.Region,
			LastActive:  binding.LastActive,
		})
	}
	return result
}

// Count 返回活动（未过期）绑定的数量。该操作只读：
// 不会刷新 LastActive，也不会删除过期条目。
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, binding := range s.bindings {
		if !s.expired(binding) {
			count++
		}
	}
	return count
}

// TTL 返回配置的会话存活时间；UI 可结合 SessionBinding.LastActive 计算倒计时。
func (s *Store) TTL() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ttl
}

func (s *Store) SetTTL(ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ttl = ttl
}

// RemainingTTL 根据存储时钟返回指定绑定距过期的剩余时间。
// 绑定已到期或超过过期时间时返回 0；未配置 TTL（ttl <= 0）时也返回 0。
// 该操作只读。
func (s *Store) RemainingTTL(binding SessionBinding) time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ttl <= 0 {
		return 0
	}
	remaining := s.ttl - s.now().Sub(binding.LastActive)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Now 返回存储时钟的当前时刻（测试中可注入）。
func (s *Store) Now() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.now()
}
