// Package jobstore 为 ofd-server 提供基于 bbolt 的任务持久化与队列。
//
// 选 bbolt 的原因：单文件、事务是 ACID 的、纯 Go 不需要 cgo，适合与本项目现有
// 构建矩阵共存。代价是它对数据库文件加排他锁，**同一文件只能由一个进程打开**，
// 因此本服务按单节点设计；需要多副本时必须换成 SQLite 或外部队列。
//
// 四类数据分四个 bucket：
//
//	meta          schema_version
//	jobs          jobID → JSON 任务记录
//	queue_fast    快速路径的待办，按创建时间排序
//	queue_heavy   重路径的待办（Office/HTML 走 LibreOffice/Chrome）
//	jobs_by_state <state><创建时间><jobID>，按状态查与清理
//
// 队列键与状态索引都用大端时间戳，保证 bbolt 的字节序比较等价于时间序。
// 所有会同时改动多个 bucket 的操作都在单个写事务内完成：bbolt 串行化写事务，
// 因此多个 worker 并发取件是安全的。
package jobstore

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/goccy/go-json/jsontext"
	"github.com/goccy/go-json/v2"

	bolt "go.etcd.io/bbolt"
)

// Lane 区分任务队列。快路径与重路径分开，是为了避免一个 LibreOffice 请求
// 把 Markdown/文本这类毫秒级任务一起堵住。
type Lane string

const (
	// LaneFast 是快路径：OFD、PDF、Markdown、文本与图片等纯 Go 转换。
	LaneFast Lane = "fast"
	// LaneHeavy 是重路径：Office 文档经 LibreOffice、HTML/MHTML 经 Chrome。
	LaneHeavy Lane = "heavy"
)

// State 是任务状态。
type State string

const (
	StateQueued    State = "queued"
	StateRunning   State = "running"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

// Terminal 报告该状态是否已终结。终结的任务不再回到队列。
func (s State) Terminal() bool {
	switch s {
	case StateSucceeded, StateFailed, StateCancelled:
		return true
	}
	return false
}

// stateCode 把状态映射为索引键里的单字节。用显式字母而不是字符串首字节，状态名
// 一旦改动也不会让已有索引失效；字母本身在排查时也比 0/1/2 好读。
var stateCodes = map[State]byte{
	StateQueued:    'q',
	StateRunning:   'r',
	StateSucceeded: 's',
	StateFailed:    'f',
	StateCancelled: 'c',
}

// statesByCode 是 stateCodes 的反查表，供状态索引扫描时确认键的前缀属于哪个状态。
var statesByCode = func() map[byte]State {
	reverse := make(map[byte]State, len(stateCodes))
	for state, code := range stateCodes {
		reverse[code] = state
	}
	return reverse
}()

func stateCode(state State) byte { return stateCodes[state] }

var (
	bucketMeta    = []byte("meta")
	bucketJobs    = []byte("jobs")
	bucketFast    = []byte("queue_fast")
	bucketHeavy   = []byte("queue_heavy")
	bucketByState = []byte("jobs_by_state")
	bucketDelayed = []byte("queue_delayed")
	// bucketIdempotency 保存 Idempotency-Key 到任务 ID 的映射。
	//
	// 键是调用方提供的幂等键，值是 {job_id, fingerprint, created_at}。映射与
	// 任务写在同一事务里，重放时不会出现“键已占住但任务没入队”的中间态。
	bucketIdempotency = []byte("idempotency")
	// bucketStats 存单调累计计数，不参与 Prune。
	//
	// 单独一个 bucket 是必须的：如果从 jobs 表现算，任务被清理后计数会
	// 往下掉，而 Prometheus 的 counter 一旦出现下降，rate() 与 increase()
	// 产出的就是错值——而且不会报错，图看着有数据实际全错。
	bucketStats = []byte("stats")

	schemaVersionKey = []byte("schema_version")
	schemaVersion    = 1
)

// 面向 HTTP 层的哨兵错误。调用方用 errors.Is 判断，不再匹配错误字符串：
// 文案会变，而且数据库错误不该被误判成"任务不可取消"。
var (
	ErrJobNotFound    = errors.New("任务不存在")
	ErrNotCancellable = errors.New("任务不可取消")
	ErrJobNotTerminal = errors.New("任务尚未终结")
	// ErrQueueFull 表示排队任务已达到配置上限，调用方应稍后重试。
	ErrQueueFull = errors.New("队列已满")
	// ErrIdempotencyConflict 表示同一个 Idempotency-Key 被用于不同请求。
	ErrIdempotencyConflict = errors.New("幂等键已用于其他请求")
)

func laneBucket(lane Lane) []byte {
	switch lane {
	case LaneHeavy:
		return bucketHeavy
	default:
		return bucketFast
	}
}

// Order 是队列的出队顺序。同一优先级下按创建时间先进先出。
type Order int

const (
	// OrderNormal 是普通任务。
	OrderNormal Order = iota
	// OrderHigh 让任务插到普通任务之前。预留给交互式转换。
	OrderHigh
)

var orderPrefix = map[Order]byte{
	OrderHigh:   0x00,
	OrderNormal: 0x01,
}

// Job 是一次转换任务的持久化记录。Request 保存原始请求体，worker 取出后据此
// 重建 Source/Sink——重试时需要能完全重放。
type Job struct {
	ID    string `json:"id"`
	State State  `json:"state"`
	Lane  Lane   `json:"lane"`
	Order Order  `json:"order"`
	// From 是提交时调用方声明的输入格式，可能为空——大多数请求靠服务端
	// 按魔数与文件名推断，声明值只是覆盖手段。
	From string `json:"from"`
	// FromActual 是服务端实际判定的输入格式，成功后回写。
	//
	// 不能拿 From 当统计口径：README 里写明输入格式由服务端解析、不采信
	// 调用方的文件名，而 From 恰好就是那个声明值。一个 OFD 内容配了
	// .html 名字、按 html 导入器转换的任务，在 From 里会记成 ofd→pdf。
	// 空表示尚未判定（任务失败或仍在排队）。
	FromActual string `json:"from_actual,omitempty"`
	To         string `json:"to"`
	// InputBytes 是输入字节数。
	//
	// 提交时先记已收到的部分（上传的字节，URL 输入为 0），转换成功后用
	// 实际落盘的尺寸覆盖，所以 URL 输入失败时这一项是 0——不是漏记，
	// 是那时确实没有拿到完整内容。
	InputBytes int64 `json:"input_bytes,omitzero"`
	// Request 是提交时的原始请求 JSON。
	Request jsontext.Value `json:"request,omitempty"`
	// Output 是终态时的结果位置。
	Output Output `json:"output,omitempty"`
	// Error 是失败原因，终态时对调用方可见。
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	// DueAt 是任务最早可被取走的时刻。为零表示立即可取；重试时用它实现退避，
	// 靠把它塞进 delayed 队列实现，而不是靠 sleep——sleep 会占住一个 worker。
	DueAt        time.Time `json:"due_at,omitempty"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	FinishedAt   time.Time `json:"finished_at,omitempty"`
	Attempt      int       `json:"attempt"`
	NotifyTarget string    `json:"notify_target,omitempty"`
	// NotifyEvents 限定要通知的事件，空表示用目标的默认集合。
	NotifyEvents []string `json:"notify_events,omitempty"`
}

// Output 描述结果位置，与 transfer.Location 字段对应，但不直接复用——任务记录要
// 保持结构稳定，不随传输层实现变化。
type Output struct {
	Kind   string `json:"kind"`
	Path   string `json:"path,omitempty"`
	Bucket string `json:"bucket,omitempty"`
	Key    string `json:"key,omitempty"`
	URL    string `json:"url,omitempty"`
	Size   int64  `json:"size"`
	// Files 是产物的文件名清单，逐页输出会有多项。
	//
	// 单有这个字段是不够的：调用方能指定 output.filename，但产物名由格式扩展名
	// 决定，逐页输出还会带页号（thumb-0001.png）。只报目录等于让调用方还得去
	// 猜文件名或者列目录——那正是"不然用户如何知道"要解决的问题。
	Files []string `json:"files,omitempty"`
}

// Store 是任务存储。
type Store struct {
	// path 单独保存一份：bbolt 在 Close() 之后会把 db.path 清空，而 Compact 需要
	// 在关闭之后重新打开，读 db.Path() 会拿到空串。
	path   string
	mu     sync.RWMutex
	db     *bolt.DB
	closed bool
}

// Open 打开或创建指定路径的存储。
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("未指定任务库路径")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("创建任务库目录失败: %w", err)
		}
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("打开任务库失败: %w", err)
	}
	store := &Store{path: path, db: db}
	if err := db.Update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{bucketMeta, bucketJobs, bucketFast, bucketHeavy, bucketByState, bucketDelayed, bucketDeliveries, bucketStats, bucketIdempotency} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		meta := tx.Bucket(bucketMeta)
		switch version := meta.Get(schemaVersionKey); {
		case version == nil:
			return meta.Put(schemaVersionKey, []byte{byte(schemaVersion)})
		case int(version[0]) != schemaVersion:
			return fmt.Errorf("任务库版本 %d 与本程序期望的 %d 不一致", version[0], schemaVersion)
		}
		return nil
	}); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// Close 关闭数据库。
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	s.closed = true
	err := s.db.Close()
	s.db = nil
	return err
}

// Ping 检查任务库是否仍可读。readiness 不应只看进程是否启动：数据库被
// 关闭、文件损坏或所在盘掉线后，服务已经处理不了任何任务。
func (s *Store) Ping() error {
	return s.view(func(tx *bolt.Tx) error {
		if tx.Bucket(bucketMeta) == nil {
			return errors.New("任务库缺少 meta bucket")
		}
		return nil
	})
}

func (s *Store) view(fn func(*bolt.Tx) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return fmt.Errorf("任务库已关闭")
	}
	return s.db.View(fn)
}

func (s *Store) update(fn func(*bolt.Tx) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return fmt.Errorf("任务库已关闭")
	}
	return s.db.Update(fn)
}

// queueKey 构造队列键：优先级前缀 + 大端时间戳 + 任务 ID。
// 大端是为了让 bbolt 的字节序比较落在时间序上；ID 放在末尾保证同刻入队的顺序
// 稳定（同一纳秒入队的任务按 ID 排，仍是确定的）。
func queueKey(job *Job) []byte {
	key := make([]byte, 0, 1+8+len(job.ID))
	key = append(key, orderPrefix[job.Order])
	key = binary.BigEndian.AppendUint64(key, uint64(job.CreatedAt.UnixNano()))
	return append(key, job.ID...)
}

// stateKey 构造状态索引键。
func stateKey(state State, createdAt time.Time, id string) []byte {
	key := make([]byte, 0, 1+8+len(id))
	key = append(key, stateCode(state))
	key = binary.BigEndian.AppendUint64(key, uint64(createdAt.UnixNano()))
	return append(key, id...)
}

// stateIndexPrefix 返回某状态下所有键的前缀，便于 Cursor 扫描。
func stateIndexPrefix(state State) []byte {
	return []byte{stateCode(state)}
}

// Enqueue 写入新任务并放入对应队列。与状态索引在同一事务内更新。
//
// 这个方法不检查队列上限，也不处理幂等键：它同时被 worker 的重试路径使用，
// 已受理的任务不应因为队列满而失败。HTTP 入口使用 EnqueueGuarded。
func (s *Store) Enqueue(job *Job) error {
	if job == nil || job.ID == "" {
		return fmt.Errorf("任务必须有 ID")
	}
	normalizeJob(job)
	return s.update(func(tx *bolt.Tx) error {
		return putQueuedJob(tx, job)
	})
}

// EnqueueGuarded 是带入口保护的入队：队列上限与幂等键在同一个写事务里检查。
//
// 返回 replayed=true 表示幂等键已存在且请求指纹一致，stored 是第一次提交
// 创建的任务，调用方应返回同一个任务而不是新建一个。key 为空表示不启用幂等。
func (s *Store) EnqueueGuarded(job *Job, key, fingerprint string, maxQueued int) (stored *Job, replayed bool, err error) {
	if job == nil || job.ID == "" {
		return nil, false, fmt.Errorf("任务必须有 ID")
	}
	normalizeJob(job)
	err = s.update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketIdempotency)
		if key != "" {
			if raw := bucket.Get([]byte(key)); raw != nil {
				var record idempotencyRecord
				if err := json.Unmarshal(raw, &record); err != nil {
					return err
				}
				if record.Fingerprint != fingerprint {
					return fmt.Errorf("%w: %s", ErrIdempotencyConflict, key)
				}
				if existing, loadErr := loadJob(tx, record.JobID); loadErr == nil {
					stored = existing
					replayed = true
					return nil
				}
				// 任务已被 retention 或 DELETE 清掉，旧映射是垃圾。
				if err := bucket.Delete([]byte(key)); err != nil {
					return err
				}
			}
		}
		if maxQueued > 0 && queuedCountTx(tx) >= maxQueued {
			return ErrQueueFull
		}
		if err := putQueuedJob(tx, job); err != nil {
			return err
		}
		if key != "" {
			raw, err := json.Marshal(idempotencyRecord{
				JobID:       job.ID,
				Fingerprint: fingerprint,
				CreatedAt:   job.CreatedAt,
			})
			if err != nil {
				return err
			}
			if err := bucket.Put([]byte(key), raw); err != nil {
				return err
			}
		}
		stored = job
		return nil
	})
	return stored, replayed, err
}

// idempotencyRecord 是 bucketIdempotency 的值。
type idempotencyRecord struct {
	JobID       string    `json:"job_id"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"created_at"`
}

func normalizeJob(job *Job) {
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now().UTC()
	}
	if job.Lane == "" {
		job.Lane = LaneFast
	}
	job.State = StateQueued
}

// putQueuedJob 把任务及其索引、队列键写入当前事务。
func putQueuedJob(tx *bolt.Tx, job *Job) error {
	raw, err := json.Marshal(job)
	if err != nil {
		return err
	}
	if err := putJob(tx, job, raw); err != nil {
		return err
	}
	// 未到期的任务先落在按到期时间排序的 delayed 队列里，到期后再被
	// promoteDue 搬进通道队列。
	if !job.DueAt.IsZero() && job.DueAt.After(time.Now()) {
		return tx.Bucket(bucketDelayed).Put(delayedKey(job), []byte(job.ID))
	}
	return tx.Bucket(laneBucket(job.Lane)).Put(queueKey(job), []byte(job.ID))
}

// queuedCountTx 统计尚未进入 worker 的 queued 任务。
//
// 两个通道队列与 delayed 队列的键数相加即可，不需要读任务记录：这正是
// 队列上限检查应走的快路径。running 不计入，因为 worker 已经在处理它。
func queuedCountTx(tx *bolt.Tx) int {
	total := 0
	for _, name := range [][]byte{bucketFast, bucketHeavy} {
		if bucket := tx.Bucket(name); bucket != nil {
			total += bucket.Stats().KeyN
		}
	}
	if delayed := tx.Bucket(bucketDelayed); delayed != nil {
		total += delayed.Stats().KeyN
	}
	return total
}

// delayedKey 以到期时间为主序，让"取出全部到期任务"退化为一次前缀扫描。
func delayedKey(job *Job) []byte {
	key := make([]byte, 0, 8+1+len(job.ID))
	key = binary.BigEndian.AppendUint64(key, uint64(job.DueAt.UnixNano()))
	key = append(key, orderPrefix[job.Order])
	return append(key, job.ID...)
}

// promoteDue 把已到期的延迟任务搬进各自的通道队列。now 之前的都会搬。
//
// 延迟任务单独放一个 bucket，而不是"在通道队列里跳过未到期项"：通道队列按
// 创建时间排序，混进一个 DueAt 在未来的重试任务后，最前面的未到期项会挡住后面
// 所有已到期的任务，只能全表扫描才能绕过。分开之后每次 Claim 的成本仍是 O(1)。
//
// promoteDue 把已到期的延迟任务搬进各自的通道队列。
//
// 做成事务内辅助而不是 Store 方法，是为了让 Claim 在同一个写事务里完成
// "提升到期任务 + 取走队首"：bbolt 的写事务是独占的，嵌套开第二个会死锁，
// 而分两次提交则存在一个窗口——两个 worker 看到同一个已到期任务。
func promoteDue(tx *bolt.Tx, now time.Time) (int, error) {
	promoted := 0
	delayed := tx.Bucket(bucketDelayed)
	if delayed == nil {
		// 缺 bucket 说明这个库文件不是本版本创建的。Open 会在下次打开时补上，
		// 这里直接返回而不是 panic：Claim 是取件主路径，不该因为一个辅助
		// bucket 缺失就让整个服务起不来。
		return 0, nil
	}
	jobs := tx.Bucket(bucketJobs)
	type entry struct {
		key []byte
		id  []byte
	}
	// 先收集键与值，再统一删除：遍历中改 bucket 会让游标失效。
	var ready []entry
	cursor := delayed.Cursor()
	for key, id := cursor.First(); key != nil; key, id = cursor.Next() {
		if len(key) < 8 {
			continue
		}
		// 键按到期时间升序，遇到还没到期的即可停止。
		if time.Unix(0, int64(binary.BigEndian.Uint64(key[:8]))).After(now) {
			break
		}
		ready = append(ready, entry{key: append([]byte(nil), key...), id: append([]byte(nil), id...)})
	}
	for _, item := range ready {
		if err := delayed.Delete(item.key); err != nil {
			return 0, err
		}
		raw := jobs.Get(item.id)
		if raw == nil {
			// 记录已被清掉，索引条目顺手丢弃即可。
			continue
		}
		var job Job
		if err := json.Unmarshal(raw, &job); err != nil {
			return 0, err
		}
		// 等待期间任务可能已被取消或完成，不必再入队。
		if job.State.Terminal() {
			continue
		}
		if err := tx.Bucket(laneBucket(job.Lane)).Put(queueKey(&job), item.id); err != nil {
			return 0, err
		}
		promoted++
	}
	return promoted, nil
}

// Claim 从指定通道取出队首任务并标记为 running。
//
// 返回 (nil, nil) 表示队列为空。取件与状态改写都在同一个写事务内完成，
// 因此多个 worker 并发调用不会取到同一个任务。
// 到期的延迟任务会先被搬进通道队列，因此重试任务到了 DueAt 就能被取走。
func (s *Store) Claim(lane Lane) (*Job, error) {
	var claimed *Job
	err := s.update(func(tx *bolt.Tx) error {
		if _, err := promoteDue(tx, time.Now()); err != nil {
			return err
		}
		queue := tx.Bucket(laneBucket(lane))
		key, id := queue.Cursor().First()
		if key == nil {
			return nil
		}
		if err := queue.Delete(key); err != nil {
			return err
		}
		job, err := loadJob(tx, string(id))
		if err != nil {
			// 队列里有、记录却没有：属于不一致数据，丢掉这个键即可，
			// 不该让整个 worker 因为一条坏数据停摆。
			return nil
		}
		if job.State.Terminal() {
			return nil
		}
		job.State = StateRunning
		job.StartedAt = time.Now().UTC()
		job.Attempt++
		raw, err := json.Marshal(job)
		if err != nil {
			return err
		}
		if err := putJob(tx, job, raw); err != nil {
			return err
		}
		claimed = job
		return nil
	})
	return claimed, err
}

func putJob(tx *bolt.Tx, job *Job, raw []byte) error {
	jobs := tx.Bucket(bucketJobs)
	byState := tx.Bucket(bucketByState)
	// 先摘掉旧的索引条目：状态或创建时间被改过时避免留下悬空索引。
	if previous := jobs.Get([]byte(job.ID)); previous != nil {
		var old Job
		if err := json.Unmarshal(previous, &old); err == nil {
			if err := byState.Delete(stateKey(old.State, old.CreatedAt, old.ID)); err != nil {
				return err
			}
		}
	}
	if err := jobs.Put([]byte(job.ID), raw); err != nil {
		return err
	}
	return byState.Put(stateKey(job.State, job.CreatedAt, job.ID), nil)
}

// Finish 写入终态。state 必须是终态，且任务当前处于 running。
//
// 不允许从 queued 直接置终态：那说明有人绕过 worker 直接改了结果，
// 与"只有 worker 能决定成败"的约定冲突。
func (s *Store) Finish(id string, state State, output Output, failure string) error {
	if !state.Terminal() {
		return fmt.Errorf("状态 %s 不是终态", state)
	}
	return s.update(func(tx *bolt.Tx) error {
		job, err := loadJob(tx, id)
		if err != nil {
			return err
		}
		if job.State.Terminal() {
			return fmt.Errorf("任务 %s 已是终态 %s", id, job.State)
		}
		job.State = state
		job.Output = output
		job.Error = failure
		job.FinishedAt = time.Now().UTC()
		// 取消不是一次转换。它仍进入终态索引，但不能进入累计转换统计；
		// 队列取消与同步转换取消都经此路径区分。
		if state != StateCancelled {
			// 计数与状态迁移同一个事务：job 是这里 load 出来的，已经带着
			// RecordUsage 写好的 FromActual 与 InputBytes，所以不需要改签名
			// 传递额外参数，也不会出现"状态已终结但用量没记上"的中间态。
			if err := accumulate(tx, job, state); err != nil {
				return err
			}
		}
		raw, err := json.Marshal(job)
		if err != nil {
			return err
		}
		return putJob(tx, job, raw)
	})
}

// accumulate 把一个终态任务的用量累加进计数。
func accumulate(tx *bolt.Tx, job *Job, state State) error {
	// 首次记账时记下时间，作为"这些数字从什么时候开始"的起点。
	// 少了它，调用方看到 1234 次转换却不知道该按什么时间跨度算速率。
	stats := tx.Bucket(bucketStats)
	if stats.Get(statsSinceKey) == nil {
		if err := stats.Put(statsSinceKey, nowBytes()); err != nil {
			return err
		}
	}
	for _, key := range statsKeys(job, state) {
		var delta uint64
		switch key[0] {
		case statKeyJobs:
			delta = 1
		case statKeyInput:
			if job.InputBytes < 0 {
				// 不该发生，但负数转 uint64 会变成天文数字，把整条 series
				// 永久污染。宁可这一项不记。
				return nil
			}
			delta = uint64(job.InputBytes)
		case statKeyOutput:
			if job.Output.Size < 0 {
				return nil
			}
			delta = uint64(job.Output.Size)
		}
		if err := addStats(tx, key, delta); err != nil {
			return err
		}
	}
	return nil
}

// RecordDirect 直接写入一条任务记录，不进任何队列。
//
// 供同步转换使用：这类转换由 HTTP 请求自己执行，没有 worker 参与，所以不能
// 经 Enqueue——那会把任务丢进通道队列，runner 就会把它领走再转换一遍，
// 等于同一个任务跑两次。
//
// 状态直接置为 running 而不是 queued：任务确实在执行中，而且这样它会出现在
// 队列深度的 running 计数里，运维能看到"有一个同步转换在跑"。
func (s *Store) RecordDirect(job *Job) error {
	if job == nil || job.ID == "" {
		return fmt.Errorf("任务必须有 ID")
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now().UTC()
	}
	job.State = StateRunning
	job.StartedAt = time.Now().UTC()
	return s.update(func(tx *bolt.Tx) error {
		raw, err := json.Marshal(job)
		if err != nil {
			return err
		}
		return putJob(tx, job, raw)
	})
}

// RecordUsage 回写任务的实际输入格式与输入字节数。
//
// 单独于 Finish 是因为这两个字段只有转换成功后才拿得到，而 Finish 会被
// 重试、取消、关停等路径调用。分开写让"终态"这个语义保持干净：一个方法
// 只管状态迁移，一个只管用量记账。
func (s *Store) RecordUsage(id string, fromActual string, inputBytes int64) error {
	return s.update(func(tx *bolt.Tx) error {
		job, err := loadJob(tx, id)
		if err != nil {
			return err
		}
		job.FromActual = fromActual
		job.InputBytes = inputBytes
		raw, err := json.Marshal(job)
		if err != nil {
			return err
		}
		return putJob(tx, job, raw)
	})
}

// Cancel 取消仍在排队的任务。
//
// 只允许取消 queued：已经 running 的任务正占着外部进程，中途杀掉它只会留下
// 半截输出，不如让它跑完再由调用方决定要不要丢弃结果。
func (s *Store) Cancel(id string) error {
	return s.update(func(tx *bolt.Tx) error {
		job, err := loadJob(tx, id)
		if err != nil {
			return err
		}
		if job.State != StateQueued {
			return fmt.Errorf("%w: 任务 %s 处于 %s，只有排队中的任务可以取消",
				ErrNotCancellable, id, job.State)
		}
		// 从延迟队列与通道队列里摘掉，否则 Claim 之后还会把它取出来。
		if err := tx.Bucket(bucketDelayed).Delete(delayedKey(job)); err != nil {
			return err
		}
		if err := tx.Bucket(laneBucket(job.Lane)).Delete(queueKey(job)); err != nil {
			return err
		}
		job.State = StateCancelled
		job.FinishedAt = time.Now().UTC()
		raw, err := json.Marshal(job)
		if err != nil {
			return err
		}
		return putJob(tx, job, raw)
	})
}

func (s *Store) Get(id string) (*Job, error) {
	var job *Job
	err := s.view(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bucketJobs).Get([]byte(id))
		if raw == nil {
			return nil
		}
		var loaded Job
		if err := json.Unmarshal(raw, &loaded); err != nil {
			return err
		}
		job = &loaded
		return nil
	})
	return job, err
}

func loadJob(tx *bolt.Tx, id string) (*Job, error) {
	raw := tx.Bucket(bucketJobs).Get([]byte(id))
	if raw == nil {
		return nil, fmt.Errorf("%w: %s", ErrJobNotFound, id)
	}
	var job Job
	if err := json.Unmarshal(raw, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// ListByState 按状态列出任务，limit 为 0 表示不限。用于管理接口与排障。
func (s *Store) ListByState(state State, limit int) ([]Job, error) {
	var result []Job
	err := s.view(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(bucketByState).Cursor()
		for key, _ := cursor.Seek(stateIndexPrefix(state)); key != nil; key, _ = cursor.Next() {
			if len(key) <= 1+8 || statesByCode[key[0]] != state {
				break
			}
			raw := tx.Bucket(bucketJobs).Get(key[1+8:])
			if raw == nil {
				continue
			}
			var job Job
			if err := json.Unmarshal(raw, &job); err != nil {
				continue
			}
			result = append(result, job)
			if limit > 0 && len(result) >= limit {
				break
			}
		}
		return nil
	})
	return result, err
}

// ListOptions 是任务列表的过滤条件。
type ListOptions struct {
	// State 为空表示不过滤状态。
	State State
	// Lane 为空表示不过滤通道。
	Lane Lane
	// Limit 是返回条数上限，0 时取 50，最大 200。
	Limit int
	// Cursor 是上一页返回的 next_cursor，空表示从最新一条开始。
	Cursor string
}

// ListResult 是一页任务列表。
type ListResult struct {
	Jobs       []Job
	NextCursor string
}

// List 按创建顺序倒序返回任务。任务 ID 是单调时间戳，因此直接按 ID 倒序
// 扫描 jobs bucket 即可；状态与通道再在内存里过滤。
//
// 这里没有全局时间索引，扫描会随任务总数增长。retention 默认只有 7 天，
// 且 limit 很小，换来的实现简单性比再加一个需要维护一致性的索引更划算。
func (s *Store) List(opts ListOptions) (ListResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	var result ListResult
	err := s.view(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketJobs)
		if bucket == nil {
			return nil
		}
		cursor := bucket.Cursor()
		start := []byte(opts.Cursor)
		var key, raw []byte
		if len(start) == 0 {
			key, raw = cursor.Last()
		} else {
			key, raw = cursor.Seek(start)
			if key == nil {
				key, raw = cursor.Last()
			} else if bytes.Compare(key, start) >= 0 {
				key, raw = cursor.Prev()
			}
		}
		for ; key != nil; key, raw = cursor.Prev() {
			var job Job
			if err := json.Unmarshal(raw, &job); err != nil {
				continue
			}
			if opts.State != "" && job.State != opts.State {
				continue
			}
			if opts.Lane != "" && job.Lane != opts.Lane {
				continue
			}
			result.Jobs = append(result.Jobs, job)
			if len(result.Jobs) > limit {
				break
			}
		}
		return nil
	})
	if err != nil {
		return ListResult{}, err
	}
	if len(result.Jobs) > limit {
		result.Jobs = result.Jobs[:limit]
		result.NextCursor = result.Jobs[len(result.Jobs)-1].ID
	}
	return result, nil
}

// Delete 删除一个已终结的任务记录及其索引。
//
// 产物文件不在这里删除：文件系统与 bbolt 无法放进同一个事务，由 HTTP 层在
// 拿到 job 后按记录的清单处理。这里只保证任务记录本身不再可见。
func (s *Store) Delete(id string) (*Job, error) {
	var deleted *Job
	err := s.update(func(tx *bolt.Tx) error {
		job, err := loadJob(tx, id)
		if err != nil {
			return err
		}
		if !job.State.Terminal() {
			return fmt.Errorf("%w: 任务 %s 处于 %s", ErrJobNotTerminal, id, job.State)
		}
		if err := tx.Bucket(bucketByState).Delete(stateKey(job.State, job.CreatedAt, job.ID)); err != nil {
			return err
		}
		if err := tx.Bucket(bucketJobs).Delete([]byte(id)); err != nil {
			return err
		}
		// 清掉可能残留的队列键；正常终态任务不会还在队列里，但取消与重启
		// 路径曾留下过悬空键，顺手清理比让 Claim 反复跳过更干净。
		if err := tx.Bucket(laneBucket(job.Lane)).Delete(queueKey(job)); err != nil {
			return err
		}
		if err := tx.Bucket(bucketDelayed).Delete(delayedKey(job)); err != nil {
			return err
		}
		if err := deleteIdempotencyForJob(tx, id); err != nil {
			return err
		}
		if err := deleteDeliveriesForJob(tx, id); err != nil {
			return err
		}
		deleted = job
		return nil
	})
	return deleted, err
}

func deleteIdempotencyForJob(tx *bolt.Tx, id string) error {
	bucket := tx.Bucket(bucketIdempotency)
	if bucket == nil {
		return nil
	}
	var stale [][]byte
	err := bucket.ForEach(func(key, value []byte) error {
		var record idempotencyRecord
		if err := json.Unmarshal(value, &record); err != nil {
			return nil
		}
		if record.JobID == id {
			stale = append(stale, append([]byte(nil), key...))
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, key := range stale {
		if err := bucket.Delete(key); err != nil {
			return err
		}
	}
	return nil
}

func deleteDeliveriesForJob(tx *bolt.Tx, id string) error {
	bucket := tx.Bucket(bucketDeliveries)
	if bucket == nil {
		return nil
	}
	var stale [][]byte
	err := bucket.ForEach(func(key, value []byte) error {
		var delivery Delivery
		if err := json.Unmarshal(value, &delivery); err != nil {
			return nil
		}
		if delivery.JobID == id {
			stale = append(stale, append([]byte(nil), key...))
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, key := range stale {
		if err := bucket.Delete(key); err != nil {
			return err
		}
	}
	return nil
}

// Count 统计各状态的任务数量。
func (s *Store) Count() (map[State]int, error) {
	counts := make(map[State]int, 5)
	for _, state := range []State{StateQueued, StateRunning, StateSucceeded, StateFailed, StateCancelled} {
		list, err := s.ListByState(state, 0)
		if err != nil {
			return nil, err
		}
		counts[state] = len(list)
	}
	return counts, nil
}

// Recover 把上次进程退出时停留在 running 的任务重新入队。
//
// 这类任务的转换进程已经随进程一起消失，不重排就会永远卡住。启动时调用一次。
func (s *Store) Recover() (int, error) {
	var recovered int
	err := s.update(func(tx *bolt.Tx) error {
		jobs := tx.Bucket(bucketJobs)
		cursor := tx.Bucket(bucketByState).Cursor()
		type pending struct {
			key []byte
			job Job
		}
		var items []pending
		for key, _ := cursor.Seek(stateIndexPrefix(StateRunning)); key != nil; key, _ = cursor.Next() {
			if len(key) <= 1+8 || statesByCode[key[0]] != StateRunning {
				break
			}
			raw := jobs.Get(key[1+8:])
			if raw == nil {
				continue
			}
			var job Job
			if err := json.Unmarshal(raw, &job); err != nil {
				continue
			}
			items = append(items, pending{key: append([]byte(nil), key...), job: job})
		}
		for _, item := range items {
			job := item.job
			job.State = StateQueued
			job.StartedAt = time.Time{}
			job.Error = "服务重启，任务已重新排队"
			updated, err := json.Marshal(&job)
			if err != nil {
				return err
			}
			if err := putJob(tx, &job, updated); err != nil {
				return err
			}
			if err := tx.Bucket(laneBucket(job.Lane)).Put(queueKey(&job), []byte(job.ID)); err != nil {
				return err
			}
			recovered++
		}
		return nil
	})
	return recovered, err
}

// ---------------------------------------------------------------- 统计计数
//
// 键的首字节区分指标类型，其余部分是用 \x00 分隔的标签值。标签值里不会出现
// \x00（格式名来自代码内的注册表，状态名来自枚举），所以分隔是无歧义的。
//
// 用 \x00 而不是可见分隔符，是因为可见分隔符可能出现在标签值里：键一旦
// 产生歧义，两条不同的 series 会被合并成一条，计数就错了，而且这种错误
// 只在特定格式组合下出现，极难发现。

const (
	statKeyJobs   = 'j' // job:  from \x00 to \x00 state
	statKeyInput  = 'i' // input bytes: from
	statKeyOutput = 'o' // output bytes: from \x00 to
	statLabelNone = "unknown"
	statSeparator = "\x00"
)

// statsSinceKey 是第一次记账的时间，格式与任务记录里的时间一致（UTC RFC3339）。
//
// 放在 const 块外：[]byte("since") 不是编译期常量。
var statsSinceKey = []byte("since")

// nowBytes 返回当前时间的 RFC3339 字节表示。
func nowBytes() []byte {
	return []byte(time.Now().UTC().Format(time.RFC3339))
}

// statsKeys 返回该任务涉及的计数键。
//
// from 取 FromActual 而非 From：From 是调用方声明的值，而输入格式由服务端
// 解析、不采信调用方文件名（见 Job.From 的说明）。失败的任务没有
// FromActual，统一归到 unknown——不能用声明值补，那样会把"没判出来"和
// "判成这个格式"混成一类。
func statsKeys(job *Job, state State) (keys [][]byte) {
	from := job.FromActual
	if from == "" {
		from = statLabelNone
	}
	to := job.To
	if to == "" {
		to = statLabelNone
	}
	keys = append(keys,
		statKey([]byte{statKeyJobs}, from, to, string(state)),
		statKey([]byte{statKeyInput}, from),
	)
	if job.Output.Size > 0 {
		// 失败任务没有产物，size 为 0。建一条恒为 0 的 series 没有意义，
		// 不如不建——Prometheus 里缺序列和值为 0 是两件事。
		keys = append(keys, statKey([]byte{statKeyOutput}, from, to))
	}
	return keys
}

func statKey(prefix []byte, labels ...string) []byte {
	key := make([]byte, 0, len(prefix)+len(labels)*4)
	key = append(key, prefix...)
	for i, label := range labels {
		if i > 0 {
			key = append(key, statSeparator...)
		}
		key = append(key, label...)
	}
	return key
}

// addStats 在事务内给计数加 delta。delta 为 0 时不写，避免产生恒为 0 的序列。
func addStats(tx *bolt.Tx, key []byte, delta uint64) error {
	if delta == 0 {
		return nil
	}
	bucket := tx.Bucket(bucketStats)
	var current uint64
	if raw := bucket.Get(key); len(raw) == 8 {
		current = binary.BigEndian.Uint64(raw)
	}
	next := make([]byte, 8)
	binary.BigEndian.PutUint64(next, current+delta)
	return bucket.Put(key, next)
}

// StatsSnapshot 是某一时刻的全部计数。
type StatsSnapshot struct {
	// Since 是第一次记账的时间。没有转换发生过时为零值。
	//
	// 没有它，调用方只看到一个总数却不知道该按什么时间跨度算速率——
	// "1234 次转换"在一周和一年里是完全不同的两件事。
	Since time.Time `json:"since"`
	// Jobs 按 输入格式 -> 输出格式 -> 状态 索引的转换次数。
	Jobs map[StatsTriple]uint64
	// InputBytes 按输入格式索引的输入字节总量。
	InputBytes map[string]uint64
	// OutputBytes 按 输入格式 -> 输出格式 索引的输出字节总量。
	OutputBytes map[StatsPair]uint64
	// Queue 是读取累计计数的同一事务里看到的队列深度。
	//
	// 放在同一个事务里是为了让 /metrics 和 /v1/stats 的累计计数与瞬时深度
	// 出自同一时刻。分两个事务读会出现"计数已加、深度还没减"的窗口，
	// 抓取端不必知道这个窗口，却可能据此发出错误告警。
	Queue map[State]int
	// QueueByLane 是与 Queue 同一时刻、按通道拆开的深度。
	//
	// 总数无法回答"快路径堵了还是重路径堵了"：两条 lane 的 worker 数与
	// 任务成本都不同，只告警总数会把两边的问题混在一起。
	QueueByLane map[Lane]map[State]int
}

// StatsTriple 是转换计数的三个标签。
type StatsTriple struct {
	From  string
	To    string
	State string
}

// StatsPair 是字节计数的两个标签。
type StatsPair struct {
	From string
	To   string
}

// QueueDepth 报告各非终态的任务数量。
//
// 用状态索引的前缀扫描实现，成本是 O(该状态的任务数)而不是 O(全部任务)：
// bucketByState 的键首字节是状态码，同一状态的任务在 B+ 树里连续，所以 Seek
// 之后遇到前缀变化即可停止。终态任务即使积到百万条也不影响这里的耗时。
//
// 刻意不维护"计数器再自增自减"：那需要在每个状态迁移点都记得同步，
// 漏一处就是永久性错误数字，而漏了不会报错。扫描慢一点，但读到的数
// 一定对。
func (s *Store) QueueDepth() (map[State]int, error) {
	var depth map[State]int
	err := s.view(func(tx *bolt.Tx) error {
		var err error
		depth, _, err = queueDepthTx(tx)
		return err
	})
	return depth, err
}

func queueDepthTx(tx *bolt.Tx) (map[State]int, map[Lane]map[State]int, error) {
	depth := map[State]int{}
	byLane := map[Lane]map[State]int{
		LaneFast:  {},
		LaneHeavy: {},
	}
	jobs := tx.Bucket(bucketJobs)

	// queued 不扫状态索引：就绪任务按 lane 分在两个队列 bucket 里，直接
	// 数键即可。状态索引的条目数与全部积压任务数成正比，积压大时每次
	// 抓取都全扫一遍，恰好会在最需要指标的时候拖慢 /metrics。
	if q := tx.Bucket(bucketFast); q != nil {
		byLane[LaneFast][StateQueued] = q.Stats().KeyN
	}
	if q := tx.Bucket(bucketHeavy); q != nil {
		byLane[LaneHeavy][StateQueued] = q.Stats().KeyN
	}
	// 退避中的 queued 任务不在通道队列里，只能按 ID 读记录；这里只需
	// lane 字段，用窄结构体避免把 Request 一并解出来。
	if delayed := tx.Bucket(bucketDelayed); delayed != nil && jobs != nil {
		cursor := delayed.Cursor()
		for _, value := cursor.First(); value != nil; _, value = cursor.Next() {
			addJobLane(jobs, value, StateQueued, byLane)
		}
	}
	depth[StateQueued] = byLane[LaneFast][StateQueued] + byLane[LaneHeavy][StateQueued]

	// running 必须走状态索引，但数量受 worker 数约束；逐条解码 lane 的
	// 成本随并发数增长，不随积压增长。
	running := 0
	if bucket := tx.Bucket(bucketByState); bucket != nil {
		prefix := []byte{stateCode(StateRunning)}
		cursor := bucket.Cursor()
		for key, _ := cursor.Seek(prefix); key != nil && bytes.HasPrefix(key, prefix); key, _ = cursor.Next() {
			running++
			if jobs != nil && len(key) > 1+8 {
				addJobLane(jobs, key[1+8:], StateRunning, byLane)
			}
		}
	}
	depth[StateRunning] = running
	return depth, byLane, nil
}

// addJobLane 只解码任务记录里的 lane 字段，并累加到对应通道。
func addJobLane(jobs *bolt.Bucket, id []byte, state State, byLane map[Lane]map[State]int) {
	raw := jobs.Get(id)
	if raw == nil {
		return
	}
	var meta struct {
		Lane Lane `json:"lane"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return
	}
	lane := meta.Lane
	if lane != LaneHeavy {
		lane = LaneFast
	}
	byLane[lane][state]++
}

// Snapshot 读取全部累计计数。
func (s *Store) Snapshot() (StatsSnapshot, error) {
	snap := StatsSnapshot{
		Jobs:        make(map[StatsTriple]uint64),
		InputBytes:  make(map[string]uint64),
		OutputBytes: make(map[StatsPair]uint64),
	}
	err := s.view(func(tx *bolt.Tx) error {
		stats := tx.Bucket(bucketStats)
		if raw := stats.Get(statsSinceKey); len(raw) > 0 {
			if since, err := time.Parse(time.RFC3339, string(raw)); err == nil {
				snap.Since = since
			}
		}
		if err := stats.ForEach(func(key, value []byte) error {
			if len(key) == 0 || len(value) != 8 {
				// 不是本包写的键，跳过而不是报错：同一个库可能被别的工具
				// 打开过，静默跳过比整个 /metrics 挂掉好。
				return nil
			}
			count := binary.BigEndian.Uint64(value)
			if key[0] == 's' && string(key) == string(statsSinceKey) {
				// since 不是指标，已在上面单独处理。
				return nil
			}
			labels := strings.Split(string(key[1:]), statSeparator)
			switch key[0] {
			case statKeyJobs:
				if len(labels) != 3 {
					return nil
				}
				snap.Jobs[StatsTriple{labels[0], labels[1], labels[2]}] = count
			case statKeyInput:
				if len(labels) != 1 {
					return nil
				}
				snap.InputBytes[labels[0]] = count
			case statKeyOutput:
				if len(labels) != 2 {
					return nil
				}
				snap.OutputBytes[StatsPair{labels[0], labels[1]}] = count
			}
			return nil
		}); err != nil {
			return err
		}
		var err error
		snap.Queue, snap.QueueByLane, err = queueDepthTx(tx)
		return err
	})
	return snap, err
}

// Prune 删除早于 cutoff 的终态任务，返回删除数量。运行中与排队中的任务不受影响。
// bbolt 的 mmap 只增不减：不断产生短命任务记录会让文件一直变大，因此需要定期
// 清理（配合 Store.Compact 做文件收缩）。
func (s *Store) Prune(cutoff time.Time) (int, error) {
	var removed int
	err := s.update(func(tx *bolt.Tx) error {
		jobs := tx.Bucket(bucketJobs)
		byState := tx.Bucket(bucketByState)
		for _, state := range []State{StateSucceeded, StateFailed, StateCancelled} {
			var victims [][]byte
			cursor := byState.Cursor()
			for key, _ := cursor.Seek(stateIndexPrefix(state)); key != nil; key, _ = cursor.Next() {
				if len(key) <= 1+8 || statesByCode[key[0]] != state {
					break
				}
				createdAt := int64(binary.BigEndian.Uint64(key[1 : 1+8]))
				if time.Unix(0, createdAt).After(cutoff) {
					break
				}
				victims = append(victims, append([]byte(nil), key...))
			}
			for _, key := range victims {
				id := key[1+8:]
				if err := byState.Delete(key); err != nil {
					return err
				}
				if err := jobs.Delete(id); err != nil {
					return err
				}
				removed++
			}
		}
		return pruneIdempotencyTx(tx)
	})
	return removed, err
}

// pruneIdempotencyTx 清掉指向已不存在任务的幂等映射。
//
// 不按任务逐个扫描映射桶：一次遍历足够，且 Prune 本来就在写事务里。
func pruneIdempotencyTx(tx *bolt.Tx) error {
	bucket := tx.Bucket(bucketIdempotency)
	if bucket == nil {
		return nil
	}
	jobs := tx.Bucket(bucketJobs)
	var stale [][]byte
	err := bucket.ForEach(func(key, value []byte) error {
		var record idempotencyRecord
		if err := json.Unmarshal(value, &record); err != nil {
			stale = append(stale, append([]byte(nil), key...))
			return nil
		}
		if record.JobID == "" || jobs.Get([]byte(record.JobID)) == nil {
			stale = append(stale, append([]byte(nil), key...))
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, key := range stale {
		if err := bucket.Delete(key); err != nil {
			return err
		}
	}
	return nil
}

const (
	compactMinFreeBytes = 64 << 20
	compactMinFreeRatio = 4
)

// CompactIfNeeded 在至少有 64 MiB 且占文件四分之一以上可回收空间时压缩数据库。
// bbolt 的空闲页会复用但不会归还给文件系统；按阈值触发避免每轮清理都复制数据库。
func (s *Store) CompactIfNeeded() (bool, error) {
	if s == nil {
		return false, fmt.Errorf("任务库已关闭")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return false, fmt.Errorf("任务库已关闭")
	}
	stats := s.db.Stats()
	pageSize := int64(s.db.Info().PageSize)
	freeBytes := int64(stats.FreePageN) * pageSize
	info, err := os.Stat(s.path)
	if err != nil {
		return false, fmt.Errorf("读取任务库大小失败: %w", err)
	}
	if !shouldCompact(info.Size(), freeBytes) {
		return false, nil
	}
	if err := s.compactAndReopenLocked(); err != nil {
		return false, err
	}
	return true, nil
}

func shouldCompact(fileSize, freeBytes int64) bool {
	return freeBytes >= compactMinFreeBytes && fileSize > 0 && freeBytes >= fileSize/compactMinFreeRatio
}

// Compact 把数据库收缩到实际使用量并原子替换原文件。
//
// bbolt 删除键后只把页标记为空闲，文件不会自动变小；任务记录不断增删时需要定期
// 收缩，否则文件会一直涨。
//
// Compact(dst, src *DB) 需要两个同时打开的句柄，而 bbolt 对可写文件加的是排他
// 锁，所以流程是：关掉当前句柄 → 用只读句柄重新打开源（共享锁）→ 压缩到临时
// 文件 → 替换原文件 → 重新以可写方式打开。任何一步失败都要把原库重新打开，
// 否则后续调用会因连接已关闭而全部失败。
func (s *Store) Compact() error {
	if s == nil {
		return fmt.Errorf("任务库已关闭")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return fmt.Errorf("任务库已关闭")
	}
	return s.compactAndReopenLocked()
}

func (s *Store) compactAndReopenLocked() error {
	source := s.path
	if err := s.db.Close(); err != nil {
		s.db = nil
		if reopenErr := s.reopen(); reopenErr != nil {
			return fmt.Errorf("关闭任务库失败且重开也失败: %v / %w", err, reopenErr)
		}
		return err
	}
	s.db = nil
	if err := s.compactLocked(source); err != nil {
		if reopenErr := s.reopen(); reopenErr != nil {
			return fmt.Errorf("压缩失败且重开任务库也失败: %v / %w", err, reopenErr)
		}
		return err
	}
	return s.reopen()
}

func (s *Store) compactLocked(source string) error {
	dir := filepath.Dir(source)
	tmpFile, err := os.CreateTemp(dir, ".bbolt-compact-*")
	if err != nil {
		return fmt.Errorf("创建压缩临时文件失败: %w", err)
	}
	target := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(target) }()

	readOnly := &bolt.Options{ReadOnly: true, Timeout: 5 * time.Second}
	src, err := bolt.Open(source, 0o600, readOnly)
	if err != nil {
		return fmt.Errorf("以只读方式打开任务库失败: %w", err)
	}
	defer func() { _ = src.Close() }()

	dst, err := bolt.Open(target, 0o600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return fmt.Errorf("打开压缩目标失败: %w", err)
	}
	compactErr := bolt.Compact(dst, src, 0)
	if closeErr := dst.Close(); closeErr != nil && compactErr == nil {
		compactErr = closeErr
	}
	if compactErr != nil {
		return fmt.Errorf("压缩任务库失败: %w", compactErr)
	}
	if err := os.Rename(target, source); err != nil {
		return fmt.Errorf("替换任务库失败: %w", err)
	}
	return nil
}

func (s *Store) reopen() error {
	db, err := bolt.Open(s.path, 0o600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return err
	}
	s.db = db
	s.closed = false
	return nil
}

// Lanes 列出全部队列名，供管理接口展示积压情况。
func Lanes() []Lane { return []Lane{LaneFast, LaneHeavy} }
