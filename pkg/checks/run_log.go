package checks

import (
	"context"
	"fmt"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v79/github"
	"github.com/srz-zumix/go-gh-extension/pkg/actions"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/ioutil"
)

type RunLogWalker struct {
	logFetcher gh.LogUrlFetcher
	ctx        context.Context
	client     *gh.GitHubClient
	repo       repository.Repository
	zipLog     *actions.WorkflowRunLogArchive
}

type TaskStep = github.TaskStep
type StepLog = actions.StepLog

func NewRunLogWalker(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, context any) *RunLogWalker {
	return &RunLogWalker{
		logFetcher: gh.GetWorkflowRunLogUrlFetcher(context),
		ctx:        ctx,
		client:     client,
		repo:       repo,
	}
}

func (w *RunLogWalker) Fetch(maxRedirects int) error {
	if w.logFetcher == nil {
		return fmt.Errorf("no log fetcher available for the given context")
	}

	logURL, err := w.logFetcher.FetchLogURL(w.ctx, w.client, w.repo, maxRedirects)
	if err != nil {
		return fmt.Errorf("failed to fetch log URL: %w", err)
	}

	zipReader, _, err := ioutil.DownloadZipArchive(w.ctx, logURL)
	if err != nil {
		return fmt.Errorf("failed to download zip archive: %w", err)
	}

	w.zipLog, err = actions.NewWorkflowRunLogArchive(w.ctx, zipReader)
	if err != nil {
		return fmt.Errorf("failed to create workflow run log archive: %w", err)
	}
	return nil
}

func (w *RunLogWalker) Walk(job github.WorkflowJob, fn func(step *TaskStep, stepLog *StepLog) error) error {
	if w.zipLog == nil {
		return fmt.Errorf("logs have not been fetched yet")
	}

	if job.Steps == nil {
		return fmt.Errorf("job %q has no steps", job.GetName())
	}

	stepLogs, err := w.zipLog.ListSteps(job.GetName())
	if err != nil {
		return fmt.Errorf("job %q not found in the logs: %w", job.GetName(), err)
	}

	for _, stepLog := range stepLogs {
		step, err := findTaskStepByNumber(job.Steps, stepLog.StepNumber)
		if err != nil {
			return fmt.Errorf("step number %d not found in job %q: %w", stepLog.StepNumber, job.GetName(), err)
		}
		if err := fn(step, stepLog); err != nil {
			return err
		}
	}
	return nil
}

func findTaskStepByNumber(steps []*TaskStep, stepNumber int) (*TaskStep, error) {
	for _, step := range steps {
		if step.GetNumber() == int64(stepNumber) {
			return step, nil
		}
	}
	return nil, fmt.Errorf("step number %d not found", stepNumber)
}
