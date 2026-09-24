package jobs

import (
	"time"

	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
	"github.com/sumedhaerram/aquila/internal/validate"
)

func refreshJob(job *Job) {
	settle(job.Tasks)
	if job.Canceled {
		job.Status = StatusCanceled
		job.Validated = false
		return
	}
	job.Status = jobStatus(job.Tasks)
	job.Validated = jobEarned(*job)
}

func jobEarned(job Job) bool {
	if job.Canceled || job.Status != StatusComplete {
		return false
	}
	return validate.Earned(validate.Input{
		Dirty:       job.Dirty,
		BaselineSHA: job.BaselineSHA,
		Baseline:    job.Baseline,
		Patch:       job.Patch,
		Plan:        job.Plan,
		Result:      jobEvidence(job),
		Impacted:    job.Impacted,
		Direct:      job.Direct,
		Workload:    Workload(job).Steps,
	})
}

func jobEvidence(job Job) plan.Evidence {
	steps := make([]plan.StepResult, 0, len(job.Tasks))
	for _, t := range job.Tasks {
		sr := t.Result
		if sr.ID == "" {
			sr.ID = t.PlanID
		}
		if sr.Kind == "" {
			sr.Kind = t.Kind
		}
		switch t.State {
		case StateSkipped:
			if sr.Verdict == "" {
				sr.Verdict = plan.VerdictSkipped
			}
		case StateSucceeded:
		case StateFailed:
			if sr.Verdict == "" {
				sr.Verdict = replay.VerdictIncomplete
			}
		default:
			sr.Verdict = replay.VerdictIncomplete
		}
		steps = append(steps, sr)
	}
	return plan.Evidence{Overall: plan.Overall(steps), Steps: steps}
}

func applyCancel(job *Job) {
	if job.Canceled || job.Status == StatusComplete || job.Status == StatusFailed || job.Status == StatusCanceled {
		return
	}
	job.Canceled = true
	for i := range job.Tasks {
		switch job.Tasks[i].State {
		case StatePending, StateReady:
			job.Tasks[i].State = StateSkipped
			job.Tasks[i].Err = "canceled"
		}
	}
	refreshJob(job)
}

func failDeadline(job *Job, now time.Time) bool {
	deadline := job.Deadline
	if deadline.IsZero() {
		if job.Created.IsZero() {
			return false
		}
		// Rows written before deadlines existed would otherwise hold their
		// gateway and quota forever.
		deadline = job.Created.Add(DefaultDeadline)
	}
	if !now.After(deadline) {
		return false
	}
	if job.Canceled || job.Status == StatusComplete || job.Status == StatusFailed || job.Status == StatusCanceled {
		return false
	}
	for i := range job.Tasks {
		switch job.Tasks[i].State {
		case StatePending, StateReady, StateLeased:
			job.Tasks[i].State = StateFailed
			job.Tasks[i].Err = "deadline"
			job.Tasks[i].WorkerID = ""
			job.Tasks[i].LeaseUntil = time.Time{}
		}
	}
	job.Status = StatusFailed
	job.Validated = false
	return true
}
