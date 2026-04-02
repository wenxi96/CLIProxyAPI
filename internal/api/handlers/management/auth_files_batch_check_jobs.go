package management

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

const (
	authFileBatchCheckJobStatusPending   = "pending"
	authFileBatchCheckJobStatusRunning   = "running"
	authFileBatchCheckJobStatusCompleted = "completed"
	authFileBatchCheckJobStatusFailed    = "failed"

	authFileBatchCheckJobRetention   = 2 * time.Hour
	authFileBatchCheckMinConcurrency = 1
	authFileBatchCheckMaxConcurrency = 12

	authFileBatchCheckDefaultConcurrencyMin = 2
	authFileBatchCheckDefaultConcurrencyMax = 6
)

type authFileBatchCheckJobScope struct {
	RequestedCount  int  `json:"requested_count"`
	IncludeDisabled bool `json:"include_disabled"`
	Concurrency     int  `json:"concurrency"`
}

type authFileBatchCheckJobProgress struct {
	Total           int    `json:"total"`
	Completed       int    `json:"completed"`
	Checked         int    `json:"checked"`
	Skipped         int    `json:"skipped"`
	Success         int    `json:"success"`
	Failed          int    `json:"failed"`
	Percent         int    `json:"percent"`
	CurrentName     string `json:"current_name,omitempty"`
	CurrentProvider string `json:"current_provider,omitempty"`
}

type authFileBatchCheckJobResponse struct {
	JobID        string                        `json:"job_id"`
	Status       string                        `json:"status"`
	Scope        authFileBatchCheckJobScope    `json:"scope"`
	Progress     authFileBatchCheckJobProgress `json:"progress"`
	CheckedAt    time.Time                     `json:"checked_at"`
	CreatedAt    time.Time                     `json:"created_at"`
	StartedAt    *time.Time                    `json:"started_at,omitempty"`
	FinishedAt   *time.Time                    `json:"finished_at,omitempty"`
	ErrorMessage string                        `json:"error_message,omitempty"`
	Summary      authFileBatchCheckSummary     `json:"summary"`
	Aggregate    authFileBatchCheckAggregate   `json:"aggregate"`
	Results      []authFileBatchCheckResult    `json:"results"`
	Skipped      []authFileBatchCheckSkipped   `json:"skipped"`
}

type authFileBatchCheckJob struct {
	mu           sync.RWMutex
	id           string
	status       string
	scope        authFileBatchCheckJobScope
	createdAt    time.Time
	startedAt    *time.Time
	finishedAt   *time.Time
	checkedAt    time.Time
	errorMessage string
	progress     authFileBatchCheckJobProgress
	results      []authFileBatchCheckResult
	skipped      []authFileBatchCheckSkipped
	activeAuths  map[string]string
}

func newAuthFileBatchCheckJob(id string, scope authFileBatchCheckJobScope, now time.Time) *authFileBatchCheckJob {
	return &authFileBatchCheckJob{
		id:        id,
		status:    authFileBatchCheckJobStatusPending,
		scope:     scope,
		createdAt: now,
		checkedAt: now,
		progress: authFileBatchCheckJobProgress{
			Total: scope.RequestedCount,
		},
		results:     make([]authFileBatchCheckResult, 0, scope.RequestedCount),
		skipped:     make([]authFileBatchCheckSkipped, 0),
		activeAuths: make(map[string]string),
	}
}

func (j *authFileBatchCheckJob) markRunning(now time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.startedAt == nil {
		startedAt := now.UTC()
		j.startedAt = &startedAt
	}
	j.status = authFileBatchCheckJobStatusRunning
}

func (j *authFileBatchCheckJob) addSkipped(entries []authFileBatchCheckSkipped, now time.Time) {
	if len(entries) == 0 {
		return
	}

	j.mu.Lock()
	defer j.mu.Unlock()
	j.skipped = append(j.skipped, entries...)
	j.progress.Skipped += len(entries)
	j.progress.Completed += len(entries)
	j.checkedAt = now.UTC()
	j.refreshPercentLocked()
}

func (j *authFileBatchCheckJob) startAuth(auth *coreauth.Auth) {
	j.mu.Lock()
	defer j.mu.Unlock()
	name := authFileBatchCheckName(auth)
	provider := normalizeBatchCheckProvider(auth.Provider)
	j.activeAuths[name] = provider
	j.progress.CurrentName = name
	j.progress.CurrentProvider = provider
}

func (j *authFileBatchCheckJob) finishAuth(result authFileBatchCheckResult) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.results = append(j.results, result)
	j.progress.Checked++
	j.progress.Completed++
	if isFailedBatchCheckResult(result) {
		j.progress.Failed++
	} else {
		j.progress.Success++
	}
	delete(j.activeAuths, result.Name)
	j.syncCurrentAuthLocked()
	j.checkedAt = time.Now().UTC()
	j.refreshPercentLocked()
}

func (j *authFileBatchCheckJob) complete(now time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()
	finishedAt := now.UTC()
	j.finishedAt = &finishedAt
	j.checkedAt = finishedAt
	j.status = authFileBatchCheckJobStatusCompleted
	j.activeAuths = map[string]string{}
	j.progress.CurrentName = ""
	j.progress.CurrentProvider = ""
	j.refreshPercentLocked()
}

func (j *authFileBatchCheckJob) fail(err error, now time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()
	finishedAt := now.UTC()
	j.finishedAt = &finishedAt
	j.checkedAt = finishedAt
	j.status = authFileBatchCheckJobStatusFailed
	if err != nil {
		j.errorMessage = strings.TrimSpace(err.Error())
	}
	j.activeAuths = map[string]string{}
	j.progress.CurrentName = ""
	j.progress.CurrentProvider = ""
	j.refreshPercentLocked()
}

func (j *authFileBatchCheckJob) syncCurrentAuthLocked() {
	for name, provider := range j.activeAuths {
		j.progress.CurrentName = name
		j.progress.CurrentProvider = provider
		return
	}
	j.progress.CurrentName = ""
	j.progress.CurrentProvider = ""
}

func (j *authFileBatchCheckJob) refreshPercentLocked() {
	if j.progress.Total <= 0 {
		j.progress.Percent = 100
		return
	}
	percent := (j.progress.Completed * 100) / j.progress.Total
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	j.progress.Percent = percent
}

func (j *authFileBatchCheckJob) snapshot() authFileBatchCheckJobResponse {
	j.mu.RLock()
	defer j.mu.RUnlock()

	results := make([]authFileBatchCheckResult, len(j.results))
	copy(results, j.results)
	skipped := make([]authFileBatchCheckSkipped, len(j.skipped))
	copy(skipped, j.skipped)
	sort.Slice(results, func(i, k int) bool {
		return strings.ToLower(results[i].Name) < strings.ToLower(results[k].Name)
	})
	sort.Slice(skipped, func(i, k int) bool {
		return strings.ToLower(skipped[i].Name) < strings.ToLower(skipped[k].Name)
	})

	summary := buildBatchCheckSummary(results, skipped)
	aggregate := buildBatchCheckAggregate(results, skipped)
	progress := j.progress
	if progress.Checked < len(results) {
		progress.Checked = len(results)
	}
	if progress.Skipped < len(skipped) {
		progress.Skipped = len(skipped)
	}

	return authFileBatchCheckJobResponse{
		JobID:        j.id,
		Status:       j.status,
		Scope:        j.scope,
		Progress:     progress,
		CheckedAt:    j.checkedAt,
		CreatedAt:    j.createdAt,
		StartedAt:    cloneTimePtr(j.startedAt),
		FinishedAt:   cloneTimePtr(j.finishedAt),
		ErrorMessage: j.errorMessage,
		Summary:      summary,
		Aggregate:    aggregate,
		Results:      results,
		Skipped:      skipped,
	}
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}

func isFailedBatchCheckResult(result authFileBatchCheckResult) bool {
	switch result.Classification {
	case authFileBatchCheckClassificationAPIError,
		authFileBatchCheckClassificationInvalidated401,
		authFileBatchCheckClassificationRequestFailed,
		authFileBatchCheckClassificationUnknown:
		return true
	default:
		return false
	}
}

func newBatchCheckJobID() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func resolveBatchCheckConcurrency(requested int) (int, error) {
	if requested == 0 {
		return defaultBatchCheckConcurrency(), nil
	}
	if requested < authFileBatchCheckMinConcurrency || requested > authFileBatchCheckMaxConcurrency {
		return 0, fmt.Errorf(
			"invalid concurrency: expected %d-%d",
			authFileBatchCheckMinConcurrency,
			authFileBatchCheckMaxConcurrency,
		)
	}
	return requested, nil
}

func defaultBatchCheckConcurrency() int {
	value := runtime.NumCPU() / 2
	if value < authFileBatchCheckDefaultConcurrencyMin {
		value = authFileBatchCheckDefaultConcurrencyMin
	}
	if value > authFileBatchCheckDefaultConcurrencyMax {
		value = authFileBatchCheckDefaultConcurrencyMax
	}
	return value
}

func (h *Handler) runBatchCheckConcurrently(
	ctx context.Context,
	auths []*coreauth.Auth,
	concurrency int,
	onStart func(*coreauth.Auth),
	onFinish func(authFileBatchCheckResult),
) []authFileBatchCheckResult {
	if len(auths) == 0 {
		return nil
	}
	if concurrency < authFileBatchCheckMinConcurrency {
		concurrency = authFileBatchCheckMinConcurrency
	}

	resultsCh := make(chan authFileBatchCheckResult, len(auths))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, auth := range auths {
		if auth == nil {
			continue
		}
		wg.Add(1)
		go func(currentAuth *coreauth.Auth) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() {
				<-sem
			}()

			if onStart != nil {
				onStart(currentAuth)
			}
			result := h.checkSingleAuthFile(ctx, currentAuth)
			if onFinish != nil {
				onFinish(result)
			}
			resultsCh <- result
		}(auth)
	}

	wg.Wait()
	close(resultsCh)

	results := make([]authFileBatchCheckResult, 0, len(auths))
	for result := range resultsCh {
		results = append(results, result)
	}
	return results
}

func (h *Handler) CreateBatchCheckJob(c *gin.Context) {
	if h == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "core auth manager unavailable"})
		return
	}

	var req authFileBatchCheckRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
	}

	concurrency, err := resolveBatchCheckConcurrency(req.Concurrency)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	jobID, err := newBatchCheckJobID()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create batch check job"})
		return
	}

	requestedNames := normalizeBatchCheckNames(req.Names)
	scope := authFileBatchCheckJobScope{
		RequestedCount:  authFileBatchCheckRequestedCount(requestedNames, h.authManager.List()),
		IncludeDisabled: req.IncludeDisabled,
		Concurrency:     concurrency,
	}
	job := newAuthFileBatchCheckJob(jobID, scope, time.Now().UTC())

	h.batchCheckJobsMu.Lock()
	h.pruneExpiredBatchCheckJobsLocked(time.Now().UTC())
	h.batchCheckJobs[jobID] = job
	h.batchCheckJobsMu.Unlock()

	go h.runBatchCheckJob(job, requestedNames, req.IncludeDisabled, concurrency)

	c.JSON(http.StatusAccepted, gin.H{
		"job_id":     jobID,
		"status":     job.status,
		"scope":      scope,
		"created_at": job.createdAt,
	})
}

func authFileBatchCheckRequestedCount(requestedNames []string, auths []*coreauth.Auth) int {
	if len(requestedNames) > 0 {
		return len(requestedNames)
	}
	total := 0
	for _, auth := range auths {
		if auth == nil {
			continue
		}
		total++
	}
	return total
}

func (h *Handler) GetBatchCheckJob(c *gin.Context) {
	if h == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "management handler unavailable"})
		return
	}

	jobID := strings.TrimSpace(c.Param("id"))
	if jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing job id"})
		return
	}

	h.batchCheckJobsMu.RLock()
	job, ok := h.batchCheckJobs[jobID]
	h.batchCheckJobsMu.RUnlock()
	if !ok || job == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "batch check job not found"})
		return
	}

	c.JSON(http.StatusOK, job.snapshot())
}

func (h *Handler) runBatchCheckJob(job *authFileBatchCheckJob, requestedNames []string, includeDisabled bool, concurrency int) {
	if h == nil || job == nil || h.authManager == nil {
		return
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			job.fail(fmt.Errorf("batch check job panicked: %v", recovered), time.Now().UTC())
		}
	}()

	auths := h.authManager.List()
	selectedAuths, skipped := selectAuthsForBatchCheck(auths, requestedNames, includeDisabled)

	job.markRunning(time.Now().UTC())
	job.addSkipped(skipped, time.Now().UTC())
	if len(selectedAuths) == 0 {
		job.complete(time.Now().UTC())
		return
	}

	h.runBatchCheckConcurrently(context.Background(), selectedAuths, concurrency, job.startAuth, job.finishAuth)
	job.complete(time.Now().UTC())
}

func (h *Handler) pruneExpiredBatchCheckJobsLocked(now time.Time) {
	if h == nil {
		return
	}
	for id, job := range h.batchCheckJobs {
		if job == nil {
			delete(h.batchCheckJobs, id)
			continue
		}
		snapshot := job.snapshot()
		if snapshot.Status != authFileBatchCheckJobStatusCompleted && snapshot.Status != authFileBatchCheckJobStatusFailed {
			continue
		}
		if snapshot.FinishedAt == nil {
			continue
		}
		if now.Sub(snapshot.FinishedAt.UTC()) > authFileBatchCheckJobRetention {
			delete(h.batchCheckJobs, id)
		}
	}
}
