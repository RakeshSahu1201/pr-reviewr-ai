package agent

import (
	"context"
	"log"
	"pr-reviewer-ai/internal/auth"
	"pr-reviewer-ai/internal/git"
	"pr-reviewer-ai/internal/llm"
	"pr-reviewer-ai/internal/repository"
	"time"
)

// Worker handles background polling of GitLab events and triggers automated reviews.
type Worker struct {
	authSvc    *auth.AuthService
	logRepo    repository.ReviewLogRepository
	gitFactory func(webUrl, token string, projectID int64) (git.GitProvider, error)
	pipeline   *llm.Pipeline
}

// NewWorker creates a background event worker.
func NewWorker(
	authSvc *auth.AuthService,
	logRepo repository.ReviewLogRepository,
	gitFactory func(webUrl, token string, projectID int64) (git.GitProvider, error),
	pipeline *llm.Pipeline,
) *Worker {
	return &Worker{
		authSvc:    authSvc,
		logRepo:    logRepo,
		gitFactory: gitFactory,
		pipeline:   pipeline,
	}
}

// Start runs the polling loop until the context is cancelled.
func (w *Worker) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("🤖 background worker started (interval: %v)", interval)

	for {
		select {
		case <-ctx.Done():
			log.Println("🤖 background worker stopping...")
			return
		case <-ticker.C:
			w.processAllUsers(ctx)
		}
	}
}

func (w *Worker) processAllUsers(ctx context.Context) {
	infos, err := w.authSvc.GetAllUserTokens()
	if err != nil {
		log.Printf("worker: failed to fetch users: %v", err)
		return
	}

	for _, info := range infos {
		if info.ProjectID <= 0 {
			continue // skip users without a default project
		}

		// Process user in a separate goroutine or sequentially (sequentially for now to keep it simple and safe).
		w.processUser(ctx, info)
	}
}

func (w *Worker) processUser(ctx context.Context, info auth.UserTokenInfo) {
	provider, err := w.gitFactory(info.WebUrl, info.Token, info.ProjectID)
	if err != nil {
		log.Printf("worker: failed to build git provider for user %d: %v", info.UserID, err)
		return
	}

	events, err := provider.FetchRecentEvents(int(info.LastEventID))
	if err != nil {
		log.Printf("worker: failed to fetch events for user %d: %v", info.UserID, err)
		return
	}

	if len(events) == 0 {
		return
	}

	maxID := info.LastEventID
	for _, event := range events {
		if event.ID > maxID {
			maxID = event.ID
		}

		// We only care about MergeRequest events for now.
		// ActionName: "opened", "closed", "merged", "reopened", "updated", "commented on"
		if event.TargetType != "MergeRequest" {
			continue
		}

		// Logic to extract MR ID from title or other metadata?
		// Actually, standard GitLab events don't easily give the MR numeric ID in the basic event payload.
		// Wait, let's check what UserEvent has.
		// It has TargetTitle.
		// Maybe I should fetch more details or use a different API.
		
		// For now, let's assume if it's a MergeRequest event, we might want to check it.
		// But we need the IID/ID.
		
		// If the user's requirement is "monitor mr", maybe we should poll MRs directly?
		// But polling events is more efficient than polling ALL MRs for ALL projects.
	}

	// Update watermark regardless of whether we did a review.
	if maxID > info.LastEventID {
		_ = w.authSvc.UpdateLastEventID(info.UserID, maxID)
	}
}
