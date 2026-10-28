package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/zc310/ofd/internal/server/convertersvc"
	"github.com/zc310/ofd/internal/server/jobstore"
)

const (
	defaultListLimit = 50
	maxListLimit     = 200
)

// handleJob 处理同一路径上的 GET 与 DELETE。
func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		s.handleDeleteJob(w, r)
		return
	}
	s.handleGetJob(w, r)
}

// handleListJobs 返回任务列表，支持按状态/通道过滤与 ID 游标分页。
func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	opts := jobstore.ListOptions{
		Cursor: query.Get("cursor"),
	}
	if state := jobstore.State(query.Get("state")); state != "" {
		if !validJobState(state) {
			writeError(w, http.StatusBadRequest, "invalid_request", "state 必须是 queued/running/succeeded/failed/cancelled")
			return
		}
		opts.State = state
	}
	if lane := jobstore.Lane(query.Get("lane")); lane != "" {
		if lane != jobstore.LaneFast && lane != jobstore.LaneHeavy {
			writeError(w, http.StatusBadRequest, "invalid_request", "lane 必须是 fast 或 heavy")
			return
		}
		opts.Lane = lane
	}
	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "limit 必须是正整数")
			return
		}
		if limit > maxListLimit {
			writeError(w, http.StatusBadRequest, "invalid_request",
				fmt.Sprintf("limit 不能超过 %d", maxListLimit))
			return
		}
		opts.Limit = limit
	} else {
		opts.Limit = defaultListLimit
	}

	page, err := s.store.List(opts)
	if err != nil {
		s.log.Error("列出任务失败", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "列出任务失败")
		return
	}
	jobs := make([]any, 0, len(page.Jobs))
	for i := range page.Jobs {
		jobs = append(jobs, jobView(&page.Jobs[i]))
	}
	payload := map[string]any{
		"jobs":  jobs,
		"count": len(jobs),
	}
	if page.NextCursor != "" {
		payload["next_cursor"] = page.NextCursor
	}
	writeJSON(w, http.StatusOK, payload)
}

func validJobState(state jobstore.State) bool {
	switch state {
	case jobstore.StateQueued, jobstore.StateRunning,
		jobstore.StateSucceeded, jobstore.StateFailed, jobstore.StateCancelled:
		return true
	}
	return false
}

// handleDeleteJob 删除一个已终结的任务及其本地产物。
//
// 远端产物不在这里删除：FTP/S3/WebDAV/SFTP 上的对象属于外部存储，服务端
// 没有权利用一个 HTTP 请求替对方做生命周期决策。调用方要清远端请使用目标
// 自己的生命周期策略。
func (s *Server) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathParam(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "任务 ID 不合法")
		return
	}
	job, err := s.store.Get(id)
	if err != nil {
		s.log.Error("查询任务失败", "job", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "查询失败")
		return
	}
	if job == nil {
		writeError(w, http.StatusNotFound, "job_not_found", "任务不存在")
		return
	}
	if !job.State.Terminal() {
		writeError(w, http.StatusConflict, "job_not_finished",
			fmt.Sprintf("任务处于 %s，只有终态任务可以删除", job.State))
		return
	}
	if err := s.deleteLocalArtifacts(job); err != nil {
		s.log.Error("删除本地产物失败", "job", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "删除产物失败")
		return
	}
	if _, err := s.store.Delete(id); err != nil {
		switch {
		case errors.Is(err, jobstore.ErrJobNotFound):
			writeError(w, http.StatusNotFound, "job_not_found", "任务不存在")
		case errors.Is(err, jobstore.ErrJobNotTerminal):
			writeError(w, http.StatusConflict, "job_not_finished", err.Error())
		default:
			s.log.Error("删除任务失败", "job", id, "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "删除任务失败")
		}
		return
	}
	s.log.Info("已删除任务", "job", id)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// deleteLocalArtifacts 只删除任务记录里列出的文件，不递归删除目录。
//
// output.dir 可以由调用方显式指向共享目录，直接 RemoveAll 会把别的任务产物
// 一起删掉。按清单精确删除既符合“这个任务的产物”语义，也避免目录穿越。
func (s *Server) deleteLocalArtifacts(job *jobstore.Job) error {
	if job.Output.Kind != convertersvc.OutputDir || len(job.Output.Files) == 0 {
		return nil
	}
	dir, err := s.resolveOutputDir(job)
	if err != nil {
		// 目录已被外部清理、路径越界或含符号链接时不碰文件系统；任务记录
		// 仍可删除，产物清理失败不应把任务永久卡在库里。
		s.log.Warn("产物目录不可用，跳过文件删除", "job", job.ID, "err", err)
		return nil
	}
	defer dir.Close()
	for _, name := range job.Output.Files {
		if !isRecordedFileName(name) {
			return errInvalidArtifactList
		}
		if err := dir.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("删除产物 %q 失败: %w", name, err)
		}
	}
	return nil
}
