package atsboard

import (
	"errors"
	"testing"

	"github.com/nonamecat19/jobscraper/model"
	"github.com/nonamecat19/jobscraper/ports"
)

func TestClassifyOutcome(t *testing.T) {
	cases := []struct {
		name   string
		status int
		jobs   []model.NormalizedJob
		err    error
		want   ports.EmployerOutcome
	}{
		{"success with postings", 200, []model.NormalizedJob{{}}, nil, ports.EmployerOutcomeRead},
		{"success no postings", 200, nil, nil, ports.EmployerOutcomeNoPostings},
		{"not found", 404, nil, errors.New("boom"), ports.EmployerOutcomeNotFound},
		{"unauthorized", 401, nil, errors.New("boom"), ports.EmployerOutcomeRefused},
		{"forbidden", 403, nil, errors.New("boom"), ports.EmployerOutcomeRefused},
		{"rate limited", 429, nil, errors.New("boom"), ports.EmployerOutcomeRefused},
		{"server error", 500, nil, errors.New("boom"), ports.EmployerOutcomeUnreadable},
		{"transport failure", 0, nil, errors.New("dial failed"), ports.EmployerOutcomeUnreadable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyOutcome(tc.status, tc.jobs, tc.err)
			if got != tc.want {
				t.Errorf("ClassifyOutcome(%d, %v jobs, %v) = %q, want %q", tc.status, len(tc.jobs), tc.err, got, tc.want)
			}
		})
	}
}
