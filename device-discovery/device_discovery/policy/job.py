#!/usr/bin/env python
# Copyright 2024 NetBox Labs Inc
"""Device Discovery Job Tracking."""

import threading
import uuid
from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum


class JobStatus(str, Enum):
    """Job status enumeration."""

    RUNNING = "running"
    COMPLETED = "completed"
    FAILED = "failed"


@dataclass
class Job:
    """Represents a single job execution."""

    id: str
    status: JobStatus
    reason: str | None = None
    entity_count: int | None = None
    created_at: datetime = field(default_factory=datetime.now)
    updated_at: datetime = field(default_factory=datetime.now)


class JobStore:
    """Manages jobs in memory with thread-safe operations."""

    MAX_JOBS_PER_POLICY = 5

    def __init__(self):
        """Initialize the JobStore."""
        self._lock = threading.RLock()
        self._jobs: dict[str, list[Job]] = {}  # policy_name -> jobs

    def create_job(self, policy_name: str) -> Job:
        """
        Create a new job for the given policy.

        Args:
        ----
            policy_name: Name of the policy.

        Returns:
        -------
            Job: The created job.

        """
        with self._lock:
            now = datetime.now()
            job = Job(
                id=str(uuid.uuid4()),
                status=JobStatus.RUNNING,
                created_at=now,
                updated_at=now,
            )

            # Add job to the policy's job list
            jobs = self._jobs.get(policy_name, [])
            jobs.append(job)

            # Keep only the last MAX_JOBS_PER_POLICY jobs
            if len(jobs) > self.MAX_JOBS_PER_POLICY:
                jobs = jobs[-self.MAX_JOBS_PER_POLICY :]

            self._jobs[policy_name] = jobs
            return job

    def update_job(
        self,
        policy_name: str,
        job_id: str,
        status: JobStatus,
        reason: Exception | str | None = None,
        entity_count: int | None = None,
    ) -> None:
        """
        Update the status of a job.

        Args:
        ----
            policy_name: Name of the policy.
            job_id: ID of the job to update.
            status: New status for the job.
            reason: Optional reason (exception or string).
            entity_count: Optional entity count.

        """
        with self._lock:
            jobs = self._jobs.get(policy_name, [])
            for job in jobs:
                if job.id == job_id:
                    job.status = status
                    job.updated_at = datetime.now()
                    if reason is not None:
                        if isinstance(reason, Exception):
                            job.reason = str(reason)
                        else:
                            job.reason = reason
                    if entity_count is not None:
                        job.entity_count = entity_count
                    return

    def get_jobs_for_policy(self, policy_name: str) -> list[Job]:
        """
        Get all jobs for a given policy.

        Args:
        ----
            policy_name: Name of the policy.

        Returns:
        -------
            list[Job]: List of jobs for the policy (copy to avoid race conditions).

        """
        with self._lock:
            jobs = self._jobs.get(policy_name, [])
            # Return a copy to avoid race conditions
            return [Job(**job.__dict__) for job in jobs]

    def get_all_policies_with_jobs(self) -> dict[str, list[Job]]:
        """
        Get all policies with their jobs.

        Returns
        -------
            dict[str, list[Job]]: Dictionary mapping policy names to their jobs.

        """
        with self._lock:
            result = {}
            for policy_name, jobs in self._jobs.items():
                # Return a copy to avoid race conditions
                result[policy_name] = [Job(**job.__dict__) for job in jobs]
            return result

