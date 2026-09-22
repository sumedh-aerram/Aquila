package impact

// Label is human ground truth for one defect-shaped unified diff.
type Label struct {
	ID           string
	Direct       []string
	Likely       []string
	ForbidLikely []string
	Unobserved   string
}

// CaseScore is hits against one Label. There is no risk score.
type CaseScore struct {
	ID              string
	DirectHit       int
	DirectPred      int
	DirectGold      int
	LikelyHit       int
	LikelyPred      int
	LikelyGold      int
	Misses          []string
	ForbiddenLikely []string
}

// Metrics is micro-averaged precision and recall over labeled cases.
type Metrics struct {
	DirectPrecision float64
	DirectRecall    float64
	LikelyPrecision float64
	LikelyRecall    float64
	Cases           []CaseScore
}

// Score compares a report to labeled function names.
func Score(rep Report, lab Label) CaseScore {
	cs := CaseScore{
		ID:         lab.ID,
		DirectGold: len(lab.Direct),
		LikelyGold: len(lab.Likely),
	}
	predD := nameSet(rep.Direct, "changed_lines")
	predL := nameSet(rep.Likely, "caller")
	cs.DirectPred = len(predD)
	cs.LikelyPred = len(predL)

	for _, n := range lab.Direct {
		if _, ok := predD[n]; ok {
			cs.DirectHit++
			continue
		}
		cs.Misses = append(cs.Misses, "direct:"+n)
	}
	for _, n := range lab.Likely {
		if _, ok := predL[n]; ok {
			cs.LikelyHit++
			continue
		}
		cs.Misses = append(cs.Misses, "likely:"+n)
	}
	for _, n := range lab.ForbidLikely {
		if _, ok := predL[n]; ok {
			cs.ForbiddenLikely = append(cs.ForbiddenLikely, n)
		}
	}
	if lab.Unobserved != "" {
		found := false
		for _, f := range rep.Unobserved {
			if f.Reason == lab.Unobserved {
				found = true
				break
			}
		}
		if !found {
			cs.Misses = append(cs.Misses, "unobserved:"+lab.Unobserved)
		}
	}
	return cs
}

// Aggregate micro-averages CaseScore values. Empty denominators score as 1.
func Aggregate(cases []CaseScore) Metrics {
	m := Metrics{Cases: cases}
	var dHit, dPred, dGold, lHit, lPred, lGold int
	for _, c := range cases {
		dHit += c.DirectHit
		dPred += c.DirectPred
		dGold += c.DirectGold
		lHit += c.LikelyHit
		lPred += c.LikelyPred
		lGold += c.LikelyGold
	}
	m.DirectPrecision = ratio(dHit, dPred)
	m.DirectRecall = ratio(dHit, dGold)
	m.LikelyPrecision = ratio(lHit, lPred)
	m.LikelyRecall = ratio(lHit, lGold)
	return m
}

func nameSet(fs []Finding, reason string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, f := range fs {
		if reason != "" && f.Reason != reason {
			continue
		}
		if f.Name == "" {
			continue
		}
		out[f.Name] = struct{}{}
	}
	return out
}

func ratio(hit, den int) float64 {
	if den == 0 {
		return 1
	}
	return float64(hit) / float64(den)
}
