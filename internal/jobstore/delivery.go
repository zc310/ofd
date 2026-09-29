package jobstore

import (
	"encoding/binary"
	"errors"
	"fmt"
	json "github.com/goccy/go-json"
	"time"

	bolt "go.etcd.io/bbolt"
)

// DeliveryState 是通知投递的状态。
type DeliveryState string

const (
	// DeliveryPending 等待到期投递。
	DeliveryPending DeliveryState = "pending"
	// DeliveryInflight 已取出、正在投递。进程崩溃时该记录会被 RecoverInflight
	// 重新排回 pending——投递语义是 at-least-once，重复投递由接收方按
	// X-OFD-Delivery 去重。
	DeliveryInflight DeliveryState = "inflight"
	// DeliveryDone 已确认（收到 2xx），不再重试。
	DeliveryDone DeliveryState = "done"
	// DeliveryAbandoned 超过最大重试次数，放弃投递等待人工处理。
	DeliveryAbandoned DeliveryState = "abandoned"
)

var bucketDeliveries = []byte("deliveries")

// Delivery 是一条待投递的通知。
type Delivery struct {
	// ID 全局唯一，同时作为回调里的 X-OFD-Delivery，供接收方去重。
	ID string `json:"id"`
	// JobID 关联的转换任务，仅供排障时串联。
	JobID string `json:"job_id,omitempty"`
	// Target 是预注册的通知目标名，不是 URL——URL 与密钥都在服务端侧配置，
	// 调用方碰不到，避免被当作伪造回调的凭据。
	Target string `json:"target"`
	// Event 为 "succeeded" 或 "failed"。
	Event string `json:"event"`
	// Payload 是回调请求体，已序列化好的 JSON。
	Payload json.RawMessage `json:"payload"`
	// Attempts 已经尝试的次数。
	Attempts int `json:"attempts"`
	// MaxAttempts 超过后转为 abandoned。
	MaxAttempts int `json:"max_attempts"`
	// DueAt 下次尝试时间；重试时按退避往后推。
	DueAt time.Time `json:"due_at"`
	// State 见 DeliveryState。
	State DeliveryState `json:"state"`
	// LastError 最近一次失败原因。
	LastError string `json:"last_error,omitempty"`
}

// deliveryKey 以到期时间为主序，使"取出所有到期记录"退化为一次前缀扫描。
func deliveryKey(dueAt time.Time, id string) []byte {
	key := make([]byte, 0, 8+len(id))
	key = binary.BigEndian.AppendUint64(key, uint64(dueAt.UnixNano()))
	return append(key, id...)
}

// ScheduleDeliveries 写入待投递通知。同一批写入在一个事务内完成。
func (s *Store) ScheduleDeliveries(items []Delivery) error {
	if len(items) == 0 {
		return nil
	}
	now := time.Now().UTC()
	return s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketDeliveries)
		for i := range items {
			item := items[i]
			if item.ID == "" || item.Target == "" {
				return fmt.Errorf("投递记录必须有 ID 与 Target")
			}
			if item.DueAt.IsZero() {
				item.DueAt = now
			}
			if item.State == "" {
				item.State = DeliveryPending
			}
			if item.MaxAttempts <= 0 {
				item.MaxAttempts = 3
			}
			raw, err := json.Marshal(&item)
			if err != nil {
				return err
			}
			if err := bucket.Put(deliveryKey(item.DueAt, item.ID), raw); err != nil {
				return err
			}
		}
		return nil
	})
}

// ClaimDueDeliveries 取出到期且待投递的通知，并就地标记为 inflight。
//
// 标记与取出在同一个写事务内完成，因此多个投递协程并发调用不会取到同一条。
// 取件不改变 DueAt，所以索引键保持不变，无需重排。
// limit 为 0 表示取全部到期记录。
func (s *Store) ClaimDueDeliveries(now time.Time, limit int) ([]Delivery, error) {
	var claimed []Delivery
	err := s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketDeliveries)
		type entry struct {
			key []byte
			raw []byte
		}
		// 先收集到期且处于 pending 的记录，再逐条改写：游标在写入过程中会失效。
		//
		// 非 pending 的记录只是跳过而不删除。早期实现在这里顺手删掉它们，结果是
		// "本轮没取到可投递的" 与 "队列里已经没有 pending 的" 无法区分，投递协程
		// 会把前者当成队列已空而直接退出，剩下的记录再也发不出去。已完成的记录由
		// PruneDeliveries 统一清理。
		var due []entry
		cursor := bucket.Cursor()
		for key, value := cursor.First(); key != nil; key, value = cursor.Next() {
			if len(key) < 8 {
				continue
			}
			dueAt := int64(binary.BigEndian.Uint64(key[:8]))
			// 键按到期时间升序，遇到第一条还没到期的即可停止，后面的只会更晚。
			if time.Unix(0, dueAt).After(now) {
				break
			}
			var delivery Delivery
			if err := json.Unmarshal(value, &delivery); err != nil {
				continue
			}
			if delivery.State != DeliveryPending {
				continue
			}
			due = append(due, entry{key: append([]byte(nil), key...), raw: append([]byte(nil), value...)})
			if limit > 0 && len(due) >= limit {
				break
			}
		}
		for _, item := range due {
			var delivery Delivery
			if err := json.Unmarshal(item.raw, &delivery); err != nil {
				continue
			}
			delivery.State = DeliveryInflight
			delivery.Attempts++
			raw, err := json.Marshal(&delivery)
			if err != nil {
				return err
			}
			if err := bucket.Put(item.key, raw); err != nil {
				return err
			}
			claimed = append(claimed, delivery)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

// CompleteDelivery 标记投递成功。
func (s *Store) CompleteDelivery(id string) error {
	return s.updateDelivery(id, func(d *Delivery) error {
		d.State = DeliveryDone
		return nil
	})
}

// FailDelivery 记录一次失败。已达最大重试次数时转为 abandoned，否则按退避重新
// 排回 pending。
func (s *Store) FailDelivery(id, reason string, next time.Duration) error {
	return s.updateDelivery(id, func(d *Delivery) error {
		d.LastError = reason
		if d.MaxAttempts > 0 && d.Attempts >= d.MaxAttempts {
			d.State = DeliveryAbandoned
			return nil
		}
		d.State = DeliveryPending
		d.DueAt = time.Now().UTC().Add(next)
		return nil
	})
}

func (s *Store) updateDelivery(id string, mutate func(*Delivery) error) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketDeliveries)
		cursor := bucket.Cursor()
		for key, raw := cursor.First(); key != nil; key, raw = cursor.Next() {
			if len(key) < 8 {
				continue
			}
			var delivery Delivery
			if err := json.Unmarshal(raw, &delivery); err != nil {
				continue
			}
			if delivery.ID != id {
				continue
			}
			if err := mutate(&delivery); err != nil {
				return err
			}
			updated, err := json.Marshal(&delivery)
			if err != nil {
				return err
			}
			// 状态与到期时间都可能变化，索引键必须跟着换。
			if err := bucket.Delete(key); err != nil {
				return err
			}
			return bucket.Put(deliveryKey(delivery.DueAt, delivery.ID), updated)
		}
		return fmt.Errorf("投递记录不存在: %s", id)
	})
}

// RecoverInflight 把上次进程退出时停在 inflight 的投递排回 pending。
//
// 与任务的 Recover 同一个理由：这些通知没有送达，但外部无法察觉。at-least-once
// 语义下重投是安全的，接收方按投递 ID 去重。
func (s *Store) RecoverInflight() (int, error) {
	var recovered int
	err := s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketDeliveries)
		type entry struct {
			key []byte
			raw []byte
		}
		var stale []entry
		cursor := bucket.Cursor()
		for key, raw := cursor.First(); key != nil; key, raw = cursor.Next() {
			if len(key) < 8 {
				continue
			}
			var delivery Delivery
			if err := json.Unmarshal(raw, &delivery); err != nil {
				continue
			}
			if delivery.State == DeliveryInflight {
				stale = append(stale, entry{key: append([]byte(nil), key...), raw: append([]byte(nil), raw...)})
			}
		}
		for _, item := range stale {
			var delivery Delivery
			if err := json.Unmarshal(item.raw, &delivery); err != nil {
				continue
			}
			delivery.State = DeliveryPending
			delivery.DueAt = time.Now().UTC()
			delivery.LastError = "服务重启，通知已重新排队"
			raw, err := json.Marshal(&delivery)
			if err != nil {
				return err
			}
			if err := bucket.Delete(item.key); err != nil {
				return err
			}
			if err := bucket.Put(deliveryKey(delivery.DueAt, delivery.ID), raw); err != nil {
				return err
			}
			recovered++
		}
		return nil
	})
	return recovered, err
}

// ListDeliveries 按状态列出投递记录，limit 为 0 表示不限。用于管理接口与排障。
func (s *Store) ListDeliveries(state DeliveryState, limit int) ([]Delivery, error) {
	var result []Delivery
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketDeliveries).ForEach(func(_, value []byte) error {
			var delivery Delivery
			if err := json.Unmarshal(value, &delivery); err != nil {
				return nil
			}
			if state != "" && delivery.State != state {
				return nil
			}
			result = append(result, delivery)
			if limit > 0 && len(result) >= limit {
				return errStopIteration
			}
			return nil
		})
	})
	// errStopIteration 只是提前结束遍历的信号，不算失败。
	if err == errStopIteration {
		err = nil
	}
	return result, err
}

// errStopIteration 用于在 ForEach 中提前结束。
var errStopIteration = errors.New("停止遍历")

// PruneDeliveries 删除早于 cutoff 的已完成投递，返回删除数量。
//
// abandoned 不在此清理范围内：它们代表"通知没送出去"，需要人工介入确认，抹掉
// 记录就看不出曾经漏过。
func (s *Store) PruneDeliveries(cutoff time.Time) (int, error) {
	var removed int
	err := s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketDeliveries)
		var victims [][]byte
		cursor := bucket.Cursor()
		for key, raw := cursor.First(); key != nil; key, raw = cursor.Next() {
			if len(key) < 8 {
				continue
			}
			var delivery Delivery
			if err := json.Unmarshal(raw, &delivery); err != nil {
				continue
			}
			if delivery.State != DeliveryDone {
				continue
			}
			if delivery.DueAt.After(cutoff) {
				continue
			}
			victims = append(victims, append([]byte(nil), key...))
		}
		for _, key := range victims {
			if err := bucket.Delete(key); err != nil {
				return err
			}
			removed++
		}
		return nil
	})
	return removed, err
}
