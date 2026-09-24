package jobs

import "errors"

const (
	// DefaultActiveJobs is the per-service cap on pending or running jobs.
	DefaultActiveJobs = 4
	// DefaultLeasedTasks is the per-service cap on concurrently leased tasks.
	DefaultLeasedTasks = 2
)

// ErrQuota is returned when a service already holds its share of the fabric.
var ErrQuota = errors.New("jobs: service quota exceeded")

// Quota bounds one tenant's share of the job fabric. The tenant is the OTEL
// service.name recorded on the job; jobs without one share the empty tenant.
// Zero fields take the defaults.
type Quota struct {
	ActiveJobs  int
	LeasedTasks int
}

func (q Quota) normalized() Quota {
	if q.ActiveJobs <= 0 {
		q.ActiveJobs = DefaultActiveJobs
	}
	if q.LeasedTasks <= 0 {
		q.LeasedTasks = DefaultLeasedTasks
	}
	return q
}

func overActive(existing []Job, candidate Job, q Quota) bool {
	n := 0
	for _, j := range existing {
		if j.ID != candidate.ID && j.Service == candidate.Service && occupies(j) {
			n++
		}
	}
	return n >= q.normalized().ActiveJobs
}

// pickReady chooses the next READY task. Services with fewer leased tasks go
// first so one large DAG cannot starve another tenant; ties keep FIFO order.
// A service at its leased cap is skipped rather than queued behind.
func pickReady(all []Job, wantJob string, q Quota) (jobIdx, taskIdx int, ok bool) {
	q = q.normalized()
	leased := make(map[string]int)
	for _, j := range all {
		for _, t := range j.Tasks {
			if t.State == StateLeased {
				leased[j.Service]++
			}
		}
	}
	jobIdx, taskIdx = -1, -1
	best := 0
	for ji, j := range all {
		if wantJob != "" && j.ID != wantJob {
			continue
		}
		if j.Canceled || j.Status == StatusCanceled {
			continue
		}
		n := leased[j.Service]
		if n >= q.LeasedTasks {
			continue
		}
		if jobIdx >= 0 && n >= best {
			continue
		}
		for ti, t := range j.Tasks {
			if t.State == StateReady && !t.Operator {
				jobIdx, taskIdx, best = ji, ti, n
				break
			}
		}
	}
	return jobIdx, taskIdx, jobIdx >= 0
}
