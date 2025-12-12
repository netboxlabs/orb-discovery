package policy_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/netboxlabs/orb-discovery/snmp-discovery/policy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobStore_CreateJob(t *testing.T) {
	store := policy.NewJobStore()
	policyName := "test-policy"

	job := store.CreateJob(policyName)

	// Verify job properties
	assert.NotEmpty(t, job.ID)
	assert.Equal(t, policy.JobStatusRunning, job.Status)
	assert.Empty(t, job.Error)
	assert.False(t, job.CreatedAt.IsZero())
	assert.False(t, job.UpdatedAt.IsZero())
	assert.Equal(t, job.CreatedAt, job.UpdatedAt)

	// Verify job is stored
	jobs := store.GetJobsForPolicy(policyName)
	require.Len(t, jobs, 1)
	assert.Equal(t, job.ID, jobs[0].ID)
}

func TestJobStore_UpdateJob(t *testing.T) {
	store := policy.NewJobStore()
	policyName := "test-policy"

	job := store.CreateJob(policyName)
	jobID := job.ID

	// Update to completed
	store.UpdateJob(policyName, jobID, policy.JobStatusCompleted, nil)

	jobs := store.GetJobsForPolicy(policyName)
	require.Len(t, jobs, 1)
	assert.Equal(t, policy.JobStatusCompleted, jobs[0].Status)
	assert.Empty(t, jobs[0].Error)
	assert.True(t, jobs[0].UpdatedAt.After(jobs[0].CreatedAt))

	// Update to failed with error
	testError := errors.New("test error")
	store.UpdateJob(policyName, jobID, policy.JobStatusFailed, testError)

	jobs = store.GetJobsForPolicy(policyName)
	require.Len(t, jobs, 1)
	assert.Equal(t, policy.JobStatusFailed, jobs[0].Status)
	assert.Equal(t, testError.Error(), jobs[0].Error)
}

func TestJobStore_MaxFiveJobs(t *testing.T) {
	store := policy.NewJobStore()
	policyName := "test-policy"

	// Create 7 jobs
	var jobIDs []string
	for i := 0; i < 7; i++ {
		job := store.CreateJob(policyName)
		jobIDs = append(jobIDs, job.ID)
		time.Sleep(10 * time.Millisecond) // Small delay to ensure different timestamps
	}

	// Verify only last 5 jobs are retained
	jobs := store.GetJobsForPolicy(policyName)
	require.Len(t, jobs, 5)

	// Verify the last 5 jobs are the ones retained
	expectedIDs := jobIDs[2:] // Last 5 jobs
	actualIDs := make([]string, len(jobs))
	for i, job := range jobs {
		actualIDs[i] = job.ID
	}
	assert.Equal(t, expectedIDs, actualIDs)
}

func TestJobStore_Concurrency(t *testing.T) {
	store := policy.NewJobStore()
	policyName := "test-policy"

	var wg sync.WaitGroup
	numGoroutines := 10
	jobsPerGoroutine := 5

	// Create jobs concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < jobsPerGoroutine; j++ {
				store.CreateJob(policyName)
			}
		}()
	}

	wg.Wait()

	// Verify all jobs were created (should have max 5 per policy)
	jobs := store.GetJobsForPolicy(policyName)
	assert.LessOrEqual(t, len(jobs), 5)

	// Test concurrent updates
	if len(jobs) > 0 {
		jobID := jobs[0].ID
		wg = sync.WaitGroup{}
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				store.UpdateJob(policyName, jobID, policy.JobStatusCompleted, nil)
			}()
		}
		wg.Wait()

		// Verify job was updated
		jobs = store.GetJobsForPolicy(policyName)
		found := false
		for _, job := range jobs {
			if job.ID == jobID {
				assert.Equal(t, policy.JobStatusCompleted, job.Status)
				found = true
				break
			}
		}
		assert.True(t, found, "Job should be found after concurrent updates")
	}
}

func TestJobStore_GetAllPoliciesWithJobs(t *testing.T) {
	store := policy.NewJobStore()

	// Create jobs for multiple policies
	policy1 := "policy-1"
	policy2 := "policy-2"

	store.CreateJob(policy1)
	store.CreateJob(policy1)
	store.CreateJob(policy2)

	allJobs := store.GetAllPoliciesWithJobs()

	assert.Len(t, allJobs, 2)
	assert.Len(t, allJobs[policy1], 2)
	assert.Len(t, allJobs[policy2], 1)
}

func TestJobStore_GetJobsForPolicy_Empty(t *testing.T) {
	store := policy.NewJobStore()

	jobs := store.GetJobsForPolicy("non-existent-policy")
	assert.Empty(t, jobs)
}

func TestJobStore_UpdateJob_NonExistent(t *testing.T) {
	store := policy.NewJobStore()

	// Update a job that doesn't exist - should not panic
	store.UpdateJob("non-existent-policy", "non-existent-id", policy.JobStatusFailed, errors.New("test"))

	// Verify no jobs were created
	jobs := store.GetJobsForPolicy("non-existent-policy")
	assert.Empty(t, jobs)
}

