package agent_test

import (
	"context"
	"fmt"
	"pr-reviewer-ai/internal/agent"
	"pr-reviewer-ai/internal/git"
	"pr-reviewer-ai/internal/repository"
	"strings"
	"testing"
	"time"
)

// mockProvider implements git.GitProvider.
type mockProvider struct {
	diff          string
	fetchErr      error
	postErr       error
	postedMRID    int
	postedComment string
}

func (m *mockProvider) FetchDiff(mrID int) (string, error) { return m.diff, m.fetchErr }

func (m *mockProvider) PostReview(mrID int, comment string) error {
	m.postedMRID = mrID
	m.postedComment = comment
	return m.postErr
}

func (m *mockProvider) FetchRecentEvents(sinceEventID int) ([]git.UserEvent, error) {
	return nil, nil // not tested yet
}

// mockLogRepo implements repository.ReviewLogRepository.
type mockLogRepo struct {
	logs []repository.ReviewLog
}

func (m *mockLogRepo) LogReview(userID string, mrID int, projectID, comment string) error {
	m.logs = append(m.logs, repository.ReviewLog{
		UserID: userID, MRID: mrID, ProjectID: projectID, Comment: comment, ReviewedAt: time.Now(),
	})
	return nil
}

func (m *mockLogRepo) ListReviews(userID string) ([]repository.ReviewLog, error) {
	return m.logs, nil
}

// --- Tests ---

func TestReview_HappyPath(t *testing.T) {
	mock := &mockProvider{diff: "+added line\n-removed line\n"}
	logRepo := &mockLogRepo{}
	r := agent.New(mock, logRepo, "mygroup/myrepo", nil) // nil pipeline → stub analyser

	if err := r.Review(context.Background(), "rakesh", 42); err != nil {
		t.Fatalf("Review error: %v", err)
	}

	if mock.postedMRID != 42 {
		t.Errorf("expected PostReview called with 42, got %d", mock.postedMRID)
	}
	if !strings.Contains(mock.postedComment, "!42") {
		t.Errorf("comment should reference MR !42, got: %s", mock.postedComment)
	}
	if len(logRepo.logs) != 1 {
		t.Errorf("expected 1 audit log entry, got %d", len(logRepo.logs))
	}
}

func TestReview_FetchDiffError(t *testing.T) {
	mock := &mockProvider{fetchErr: fmt.Errorf("timeout")}
	r := agent.New(mock, nil, "proj", nil)

	if err := r.Review(context.Background(), "u", 1); err == nil {
		t.Fatal("expected error from FetchDiff failure")
	}
}

func TestReview_PostReviewError(t *testing.T) {
	mock := &mockProvider{diff: "+line\n", postErr: fmt.Errorf("forbidden")}
	r := agent.New(mock, nil, "proj", nil)

	if err := r.Review(context.Background(), "u", 7); err == nil {
		t.Fatal("expected error from PostReview failure")
	}
}

func TestReview_CommentContainsDiffStats(t *testing.T) {
	mock := &mockProvider{diff: "+line1\n+line2\n-removed\n"}
	r := agent.New(mock, nil, "proj", nil)

	_ = r.Review(context.Background(), "u", 3)

	if !strings.Contains(mock.postedComment, "+2") {
		t.Errorf("comment should mention +2, got: %s", mock.postedComment)
	}
	if !strings.Contains(mock.postedComment, "-1") {
		t.Errorf("comment should mention -1, got: %s", mock.postedComment)
	}
}

func TestReview_NilLogRepo_DoesNotPanic(t *testing.T) {
	mock := &mockProvider{diff: "+x\n"}
	r := agent.New(mock, nil, "proj", nil) // nil logRepo + nil pipeline
	if err := r.Review(context.Background(), "u", 99); err != nil {
		t.Fatalf("unexpected error with nil logRepo: %v", err)
	}
}
