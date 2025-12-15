#!/usr/bin/env python
# Copyright 2024 NetBox Labs Inc
"""NetBox Labs - Job Store Unit Tests."""

import threading
import time
from datetime import datetime

import pytest

from device_discovery.policy.job import Job, JobStatus, JobStore


def test_job_store_create_job():
    """Test creating a job with correct fields."""
    store = JobStore()
    policy_name = "test-policy"

    job = store.create_job(policy_name)

    # Verify job properties
    assert job.id is not None
    assert len(job.id) > 0
    assert job.status == JobStatus.RUNNING
    assert job.error is None
    assert job.created_at is not None
    assert job.updated_at is not None
    assert job.created_at == job.updated_at

    # Verify job is stored
    jobs = store.get_jobs_for_policy(policy_name)
    assert len(jobs) == 1
    assert jobs[0].id == job.id


def test_job_store_update_job():
    """Test updating job status and error."""
    store = JobStore()
    policy_name = "test-policy"

    job = store.create_job(policy_name)
    job_id = job.id

    # Update to completed
    store.update_job(policy_name, job_id, JobStatus.COMPLETED, None)

    jobs = store.get_jobs_for_policy(policy_name)
    assert len(jobs) == 1
    assert jobs[0].status == JobStatus.COMPLETED
    assert jobs[0].error is None
    assert jobs[0].updated_at > jobs[0].created_at

    # Update to failed with error
    test_error = Exception("test error")
    store.update_job(policy_name, job_id, JobStatus.FAILED, test_error)

    jobs = store.get_jobs_for_policy(policy_name)
    assert len(jobs) == 1
    assert jobs[0].status == JobStatus.FAILED
    assert jobs[0].error == "test error"


def test_job_store_max_five_jobs():
    """Test that only the last 5 jobs are retained per policy."""
    store = JobStore()
    policy_name = "test-policy"

    # Create 7 jobs
    job_ids = []
    for i in range(7):
        job = store.create_job(policy_name)
        job_ids.append(job.id)
        time.sleep(0.01)  # Small delay to ensure different timestamps

    # Verify only last 5 jobs are retained
    jobs = store.get_jobs_for_policy(policy_name)
    assert len(jobs) == 5

    # Verify the last 5 jobs are the ones retained
    expected_ids = job_ids[2:]  # Last 5 jobs
    actual_ids = [job.id for job in jobs]
    assert actual_ids == expected_ids


def test_job_store_concurrency():
    """Test thread-safety with concurrent operations."""
    store = JobStore()
    policy_name = "test-policy"

    num_threads = 10
    jobs_per_thread = 5

    def create_jobs():
        for _ in range(jobs_per_thread):
            store.create_job(policy_name)

    # Create jobs concurrently
    threads = []
    for _ in range(num_threads):
        thread = threading.Thread(target=create_jobs)
        threads.append(thread)
        thread.start()

    for thread in threads:
        thread.join()

    # Verify all jobs were created (should have max 5 per policy)
    jobs = store.get_jobs_for_policy(policy_name)
    assert len(jobs) <= 5

    # Test concurrent updates
    if len(jobs) > 0:
        job_id = jobs[0].id
        threads = []
        for _ in range(num_threads):
            thread = threading.Thread(
                target=lambda: store.update_job(
                    policy_name, job_id, JobStatus.COMPLETED, None
                )
            )
            threads.append(thread)
            thread.start()

        for thread in threads:
            thread.join()

        # Verify job was updated
        jobs = store.get_jobs_for_policy(policy_name)
        found = False
        for job in jobs:
            if job.id == job_id:
                assert job.status == JobStatus.COMPLETED
                found = True
                break
        assert found, "Job should be found after concurrent updates"


def test_job_store_get_all_policies_with_jobs():
    """Test getting all policies with their jobs."""
    store = JobStore()

    # Create jobs for multiple policies
    policy1 = "policy-1"
    policy2 = "policy-2"

    store.create_job(policy1)
    store.create_job(policy1)
    store.create_job(policy2)

    all_jobs = store.get_all_policies_with_jobs()

    assert len(all_jobs) == 2
    assert len(all_jobs[policy1]) == 2
    assert len(all_jobs[policy2]) == 1


def test_job_store_get_jobs_for_policy_empty():
    """Test getting jobs for a non-existent policy."""
    store = JobStore()

    jobs = store.get_jobs_for_policy("non-existent-policy")
    assert len(jobs) == 0


def test_job_store_update_job_nonexistent():
    """Test updating a job that doesn't exist - should not raise error."""
    store = JobStore()

    # Update a job that doesn't exist - should not raise
    store.update_job(
        "non-existent-policy", "non-existent-id", JobStatus.FAILED, Exception("test")
    )

    # Verify no jobs were created
    jobs = store.get_jobs_for_policy("non-existent-policy")
    assert len(jobs) == 0


def test_job_dataclass():
    """Test Job dataclass initialization."""
    now = datetime.now()
    job = Job(
        id="test-id",
        status=JobStatus.RUNNING,
        error=None,
        created_at=now,
        updated_at=now,
    )

    assert job.id == "test-id"
    assert job.status == JobStatus.RUNNING
    assert job.error is None
    assert job.created_at == now
    assert job.updated_at == now


def test_job_status_enum():
    """Test JobStatus enum values."""
    assert JobStatus.RUNNING.value == "running"
    assert JobStatus.COMPLETED.value == "completed"
    assert JobStatus.FAILED.value == "failed"

