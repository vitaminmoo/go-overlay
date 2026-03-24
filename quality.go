package overlay

import "time"

// QualityLevel controls how much the renderer draws per frame.
type QualityLevel int

const (
	QualityFull    QualityLevel = iota
	QualityReduced              // skip expensive details
	QualityMinimal              // bare minimum rendering
)

func (q QualityLevel) String() string {
	switch q {
	case QualityFull:
		return "Full"
	case QualityReduced:
		return "Reduced"
	case QualityMinimal:
		return "Minimal"
	default:
		return "Unknown"
	}
}

// AdaptiveQualityOptions configures adaptive quality scaling.
type AdaptiveQualityOptions struct {
	// FrameBudget is the target frame time. Frames exceeding this budget
	// trigger quality downgrade. Default: 8ms (~120fps).
	FrameBudget time.Duration
	// DowngradeAfter is how many consecutive over-budget frames before
	// downgrading quality. Default: 3.
	DowngradeAfter int
}

type qualityTracker struct {
	opts            AdaptiveQualityOptions
	level           QualityLevel
	overBudgetCount int
}

func newQualityTracker(opts *AdaptiveQualityOptions) *qualityTracker {
	if opts == nil {
		return nil
	}
	budget := opts.FrameBudget
	if budget == 0 {
		budget = 8 * time.Millisecond
	}
	after := opts.DowngradeAfter
	if after == 0 {
		after = 3
	}
	return &qualityTracker{
		opts: AdaptiveQualityOptions{
			FrameBudget:    budget,
			DowngradeAfter: after,
		},
	}
}

func (qt *qualityTracker) recordFrame(dur time.Duration) {
	if dur > qt.opts.FrameBudget {
		qt.overBudgetCount++
		if qt.overBudgetCount >= qt.opts.DowngradeAfter && qt.level < QualityMinimal {
			qt.level++
			qt.overBudgetCount = 0
		}
	} else if dur < qt.opts.FrameBudget*3/4 {
		qt.overBudgetCount = 0
		if qt.level > QualityFull {
			qt.level--
		}
	}
}
