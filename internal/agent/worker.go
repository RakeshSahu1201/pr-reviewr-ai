package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"pr-reviewer-ai/internal/auth"
	"pr-reviewer-ai/internal/git"
	"pr-reviewer-ai/internal/llm"
	"pr-reviewer-ai/internal/repository"
)

// Worker handles background polling of GitLab events and triggers automated reviews.
type Worker struct {
	authSvc    *auth.AuthService
	logRepo    repository.ReviewLogRepository
	gitFactory func(webUrl, token string, projectID int64, gitlabUserID int) (git.GitProvider, error)
	pipeline   *llm.Pipeline
	log        *slog.Logger

	mu        sync.Mutex
	processed map[string]time.Time
}

// NewWorker creates a background event worker.
// logger should be pre-stamped with the active app_role attribute so every log
// entry emitted by the worker is traceable in production without extra decoration.
func NewWorker(
	authSvc *auth.AuthService,
	logRepo repository.ReviewLogRepository,
	gitFactory func(webUrl, token string, projectID int64, gitlabUserID int) (git.GitProvider, error),
	pipeline *llm.Pipeline,
	logger *slog.Logger,
) *Worker {
	return &Worker{
		authSvc:    authSvc,
		logRepo:    logRepo,
		gitFactory: gitFactory,
		pipeline:   pipeline,
		log:        logger,
		processed:  make(map[string]time.Time),
	}
}

// Start runs the polling loop until the context is cancelled.
func (w *Worker) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	w.log.Info("background worker started", "interval", interval)

	for {
		select {
		case <-ctx.Done():
			w.log.Info("background worker stopping")
			return
		case <-ticker.C:
			w.processAllUsers(ctx)
		}
	}
}

func (w *Worker) processAllUsers(ctx context.Context) {
	infos, err := w.authSvc.GetAllUserTokens()
	if err != nil {
		w.log.Error("failed to fetch users", "err", err)
		return
	}

	for _, info := range infos {
		if info.ProjectID <= 0 {
			continue // skip users without a default project
		}

		// Process users sequentially to keep logic simple and safe.
		w.processUser(ctx, info)
	}
}

type EventDetail struct {
	Target      string
	Description string
}

func (w *Worker) processUser(ctx context.Context, info auth.UserTokenInfo) {
	provider, err := w.gitFactory(info.WebUrl, info.Token, info.ProjectID, info.GitlabUserID)
	if err != nil {
		w.log.Error("failed to build git provider", "user_id", info.UserID, "err", err)
		return
	}

	// Fetch only events from the last window.
	events, err := provider.FetchRecentEvents(time.Now().Add(-5 * time.Minute))
	if err != nil {
		w.log.Error("failed to fetch events", "user_id", info.UserID, "err", err)
		return
	}

	if len(events) == 0 {
		return
	}

	w.log.Info("events fetched", "user_id", info.UserID, "events_count", len(events))

	for _, event := range events {
		w.handleEvent(ctx, provider, info, event)
	}
}

// handleEvent dispatches the event to the appropriate handler based on action and type.
func (w *Worker) handleEvent(ctx context.Context, provider git.GitProvider, info auth.UserTokenInfo, event git.UserEvent) {
	// Deduplicate events to avoid processing the same comment repeatedly
	hash := fmt.Sprintf("%d:%s:%s:%d:%s", info.UserID, event.ActionName, event.TargetTitle, event.NoteableIID, event.Body)

	w.mu.Lock()
	if _, ok := w.processed[hash]; ok {
		w.mu.Unlock()
		return // Already processed this event
	}

	// Clean up old entries
	now := time.Now()
	for k, v := range w.processed {
		if now.Sub(v) > 10*time.Minute {
			delete(w.processed, k)
		}
	}
	w.processed[hash] = now
	w.mu.Unlock()

	detail := w.extractEventDetails(event)
	w.log.Info(
		"processing event",
		"target", detail.Target,
		"action", event.ActionName,
		"body", event.Body,
		"noteable_id", event.NoteableID,
		"noteable_iid", event.NoteableIID,
	)

	switch event.ActionName {
	case "commented on":
		// Task: Handle review triggers via MR comments.
		if strings.Contains(event.Body, "review") {
			w.handleMergeRequestComment(ctx, provider, info, event)
		}

	case "opened":
		// Task: Future implementation for auto-reviewing new/reopened MRs.
		w.log.Info("new/reopened MR observed", "target", detail.Target)

	case "updated":
		// Task: Future implementation for re-reviewing on code updates.
		w.log.Info("MR update observed", "target", detail.Target)

	default:
		w.log.Debug("ignoring MR action", "action", event.ActionName)
	}
}

// extractEventDetails maps event fields to a generic detail structure for logging/extraction.
func (w *Worker) extractEventDetails(event git.UserEvent) EventDetail {
	var detail EventDetail
	switch event.ActionName {
	case "commented on":
		detail.Target = event.TargetTitle
		detail.Description = event.Body
	default:
		detail.Target = event.TargetTitle
		detail.Description = event.ActionName
	}
	return detail
}

// handleMergeRequestComment contains the logic for triggering a RAG-based review from a comment.
func (w *Worker) handleMergeRequestComment(ctx context.Context, provider git.GitProvider, info auth.UserTokenInfo, event git.UserEvent) {
	w.log.Info("review comment detected — processing",
		"user_id", info.UserID,
		"target_title", event.TargetTitle,
		"comment_body", event.Body,
	)

	// Because the logic is fully modularized, all we have to do is instantiate 
	// the Reviewer and call Review(). The Reviewer handles fetching the diff, 
	// running the LLM pipeline, posting the comment, and saving to the database.
	projectIDStr := strconv.FormatInt(info.ProjectID, 10)
	rev := New(provider, w.logRepo, projectIDStr, w.pipeline)

	if err := rev.Review(ctx, info.UserID, int(event.NoteableIID)); err != nil {
		w.log.Error("failed to process review", "user_id", info.UserID, "mr_iid", event.NoteableIID, "err", err)
	} else {
		w.log.Info("review successfully posted to GitLab", "mr_iid", event.NoteableIID)
	}
}

// parseMRIID extracts a numeric MR IID from a GitLab event title.
// It scans space-separated tokens for a "!<n>" prefix or a bare integer.
// Examples that match:  "!42"  "Fix login bug !42"  "42"
func parseMRIID(title string) (int, error) {
	for _, token := range strings.Fields(title) {
		token = strings.TrimPrefix(token, "!")
		token = strings.Trim(token, ".,;:")
		if n, err := strconv.Atoi(token); err == nil && n > 0 {
			return n, nil
		}
	}
	return 0, strconv.ErrSyntax
}
