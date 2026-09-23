package jobs

import (
	"net/url"
	"strings"
)

func occupancyKey(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return strings.ToLower(strings.TrimSpace(raw))
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
}

func occupancyKeys(j Job) []string {
	a := occupancyKey(j.Baseline)
	b := occupancyKey(j.Patch)
	if a == b {
		return []string{a}
	}
	return []string{a, b}
}

func occupies(j Job) bool {
	switch j.Status {
	case StatusPending, StatusRunning:
		return true
	default:
		return false
	}
}

func gatewayBusy(existing []Job, candidate Job) bool {
	want := make(map[string]struct{}, 2)
	for _, k := range occupancyKeys(candidate) {
		if k != "" {
			want[k] = struct{}{}
		}
	}
	for _, j := range existing {
		if j.ID == candidate.ID || !occupies(j) {
			continue
		}
		for _, k := range occupancyKeys(j) {
			if _, ok := want[k]; ok {
				return true
			}
		}
	}
	return false
}
