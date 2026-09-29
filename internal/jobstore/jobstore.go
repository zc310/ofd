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
	"fmt"
	json "github.com/goccy/go-json"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	// bucketStats 存单调累计计数，不参与 Prune。
	//
	// 单独一个 bucket 是必须的：如果从 jobs 表现算，任务被清理后计数会
	// 往下掉，而 Prometheus 的 counter 一旦出现下降，rate() 与 increase()
	// 产出的就是错值——而且不会报错，图看着有数据实际全错。
	bucketStats = []byte("stats")

	schemaVersionKey = []byte("schema_version")
	schemaVersion    = 1
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
	InputBytes int64 `json:"input_bytes,omitempty"`
	// Request 是提交时的原始请求 JSON。
	Request json.RawMessage `json:"request,omitempty"`
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
}

// Store 是任务存储。
type Store struct {
	// path 单独保存一份：bbolt 在 Close() 之后会把 db.path 清空，而 Compact 需要
	// 在关闭之后重新打开，读 db.Path() 会拿到空串。
	path   string
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
		for _, name := range [][]byte{bucketMeta, bucketJobs, bucketFast, bucketHeavy, bucketByState, bucketDelayed, bucketDeliveries, bucketStats} {
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
	if s == nil || s.db == nil {
		return nil
	}
	s.closed = true
	return s.db.Close()
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
func (s *Store) Enqueue(job *Job) error {
	if job == nil || job.ID == "" {
		return fmt.Errorf("任务必须有 ID")
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now().UTC()
	}
	if job.Lane == "" {
		job.Lane = LaneFast
	}
	job.State = StateQueued
	return s.db.Update(func(tx *bolt.Tx) error {
		raw, err := json.Marshal(job)
		if err != nil {
			return err
		}
		if err := putJob(tx, job, raw); err != nil {
			return err
		}
		// 未到期的任务先落在按到期时间排序的 delayed 队列里，
		// 到期后再被 promoteDue 搬进通道队列。
		if !job.DueAt.IsZero() && job.DueAt.After(time.Now()) {
			return tx.Bucket(bucketDelayed).Put(delayedKey(job), []byte(job.ID))
		}
		return tx.Bucket(laneBucket(job.Lane)).Put(queueKey(job), []byte(job.ID))
	})
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
	err := s.db.Update(func(tx *bolt.Tx) error {
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
	return s.db.Update(func(tx *bolt.Tx) error {
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
		// 计数与状态迁移同一个事务：job 是这里 load 出来的，已经带着
		// RecordUsage 写好的 FromActual 与 InputBytes，所以不需要改签名
		// 传递额外参数，也不会出现"状态已终结但用量没记上"的中间态。
		// 这比另开一个事务更便宜——Finish 本来就要写盘。
		if err := accumulate(tx, job, state); err != nil {
			return err
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

// RecordUsage 回写任务的实际输入格式与输入字节数。
//
// 单独于 Finish 是因为这两个字段只有转换成功后才拿得到，而 Finish 会被
// 重试、取消、关停等路径调用。分开写让"终态"这个语义保持干净：一个方法
// 只管状态迁移，一个只管用量记账。
func (s *Store) RecordUsage(id string, fromActual string, inputBytes int64) error {
	return s.db.Update(func(tx *bolt.Tx) error {
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
	return s.db.Update(func(tx *bolt.Tx) error {
		job, err := loadJob(tx, id)
		if err != nil {
			return err
		}
		if job.State != StateQueued {
			return fmt.Errorf("任务 %s 处于 %s，只有排队中的任务可以取消", id, job.State)
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
	err := s.db.View(func(tx *bolt.Tx) error {
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
		return nil, fmt.Errorf("任务不存在: %s", id)
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
	err := s.db.View(func(tx *bolt.Tx) error {
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
	err := s.db.Update(func(tx *bolt.Tx) error {
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

// statsKeys 返回该任务涉及的计数键。
//
// from 取 FromActual 而非 From：From 是调用方声明的值，而输入格式由服务端
// 解析、不采信调用方文件名（见 Job.From 的说明）。失败的��务没有
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
	// Jobs 按 输入格式 -> 输出格式 -> 状态 索引的转换次数。
	Jobs map[StatsTriple]uint64
	// InputBytes 按输入格式索引的输入字节总量。
	InputBytes map[string]uint64
	// OutputBytes 按 输入格式 -> 输出格式 索引的输出字节总量。
	OutputBytes map[StatsPair]uint64
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
	depth := map[State]int{}
	err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketByState)
		if bucket == nil {
			return nil
		}
		for _, state := range []State{StateQueued, StateRunning} {
			prefix := []byte{stateCode(state)}
			count := 0
			cursor := bucket.Cursor()
			for key, _ := cursor.Seek(prefix); key != nil && bytes.HasPrefix(key, prefix); key, _ = cursor.Next() {
				count++
			}
			depth[state] = count
		}
		return nil
	})
	return depth, err
}

// Snapshot 读取全部累计计数。
func (s *Store) Snapshot() (StatsSnapshot, error) {
	snap := StatsSnapshot{
		Jobs:        make(map[StatsTriple]uint64),
		InputBytes:  make(map[string]uint64),
		OutputBytes: make(map[StatsPair]uint64),
	}
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketStats).ForEach(func(key, value []byte) error {
			if len(key) == 0 || len(value) != 8 {
				// 不是本包写的键，跳过而不是报错：同一个库可能被别的工具
				// 打开过，静默跳过比整个 /metrics 挂掉好。
				return nil
			}
			count := binary.BigEndian.Uint64(value)
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
		})
	})
	return snap, err
}

// Prune 删除早于 cutoff 的终态任务，返回删除数量。运行中与排队中的任务不受影响。
// bbolt 的 mmap 只增不减：不断产生短命任务记录会让文件一直变大，因此需要定期
// 清理（配合 Store.Compact 做文件收缩）。
func (s *Store) Prune(cutoff time.Time) (int, error) {
	var removed int
	err := s.db.Update(func(tx *bolt.Tx) error {
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
		return nil
	})
	return removed, err
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
	if s.db == nil {
		return fmt.Errorf("任务库已关闭")
	}
	source := s.path

	if err := s.db.Close(); err != nil {
		return err
	}
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
