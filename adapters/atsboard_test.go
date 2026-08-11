package adapters

import (
	"errors"
	"testing"

	"github.com/nonamecat19/jobscraper/adapter"
	"github.com/nonamecat19/jobscraper/model"
)

func TestClassifyOutcome(t *testing.T) {
	cases := []struct {
		name   string
		status int
		jobs   []model.NormalizedJob
		err    error
		want   adapter.EmployerOutcome
	}{
		{"success with postings", 200, []model.NormalizedJob{{}}, nil, adapter.EmployerOutcomeRead},
		{"success no postings", 200, nil, nil, adapter.EmployerOutcomeNoPostings},
		{"not found", 404, nil, errors.New("boom"), adapter.EmployerOutcomeNotFound},
		{"unauthorized", 401, nil, errors.New("boom"), adapter.EmployerOutcomeRefused},
		{"forbidden", 403, nil, errors.New("boom"), adapter.EmployerOutcomeRefused},
		{"rate limited", 429, nil, errors.New("boom"), adapter.EmployerOutcomeRefused},
		{"server error", 500, nil, errors.New("boom"), adapter.EmployerOutcomeUnreadable},
		{"transport failure", 0, nil, errors.New("dial failed"), adapter.EmployerOutcomeUnreadable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyOutcome(tc.status, tc.jobs, tc.err)
			if got != tc.want {
				t.Errorf("classifyOutcome(%d, %v jobs, %v) = %q, want %q", tc.status, len(tc.jobs), tc.err, got, tc.want)
			}
		})
	}
}
