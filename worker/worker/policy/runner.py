#!/usr/bin/env python
# Copyright 2025 NetBox Labs Inc
"""Orb Worker Policy Runner."""

import logging
import time
from datetime import datetime, timedelta

from apscheduler.schedulers.background import BackgroundScheduler
from apscheduler.triggers.cron import CronTrigger
from apscheduler.triggers.date import DateTrigger
from netboxlabs.diode.sdk import DiodeClient, DiodeDryRunClient, DiodeOTLPClient
from netboxlabs.diode.sdk.diode.v1 import ingester_pb2

from worker.backend import Backend, load_class
from worker.metrics import get_metric
from worker.models import DiodeConfig, Policy, Status
from worker.policy.job import JobStatus, JobStore

# Set up logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

TARGET_CHUNK_SIZE = 3.5


class PolicyRunner:
    """Policy Runner class."""

    def __init__(self):
        """Initialize the PolicyRunner."""
        self.name = ""
        self.metadata = None
        self.policy = None
        self.status = Status.NEW
        self.scheduler = BackgroundScheduler()
        self.job_store: JobStore | None = None

    def setup(self, name: str, diode_config: DiodeConfig, policy: Policy, job_store: JobStore | None = None):
        """
        Set up the policy runner.

        Args:
        ----
            name: Policy name.
            diode_config: Diode configuration data.
            policy: Policy configuration data.
            job_store: Optional JobStore for tracking jobs.

        """
        self.job_store = job_store
        self.name = name.replace("\r\n", "").replace("\n", "")
        policy.config.package = policy.config.package.replace("\r\n", "").replace(
            "\n", ""
        )
        backend_class = load_class(policy.config.package)
        backend = backend_class()

        metadata = backend.setup()
        app_name = (
            f"{diode_config.prefix}/{metadata.app_name}"
            if diode_config.prefix
            else metadata.app_name
        )
        if diode_config.dry_run:
            client = DiodeDryRunClient(
                app_name=app_name,
                output_dir=diode_config.dry_run_output_dir,
            )
        elif diode_config.client_id is not None and diode_config.client_secret is not None:
            client = DiodeClient(
                target=diode_config.target,
                app_name=app_name,
                app_version=metadata.app_version,
                client_id=diode_config.client_id,
                client_secret=diode_config.client_secret,
            )
        else:
            logger.debug("Initializing Diode OTLP client")
            client = DiodeOTLPClient(
                target=diode_config.target,
                app_name=app_name,
                app_version=metadata.app_version,
            )

        self.metadata = metadata
        self.policy = policy

        self.scheduler.start()

        if self.policy.config.schedule is not None:
            logger.info(
                f"Policy {self.name}, Package {self.policy.config.package}: Scheduled to run with '{self.policy.config.schedule}'"
            )
            trigger = CronTrigger.from_crontab(self.policy.config.schedule)
        else:
            logger.info(
                f"Policy {self.name}, Package {self.policy.config.package}: One-time run"
            )
            trigger = DateTrigger(run_date=datetime.now() + timedelta(seconds=1))

        self.scheduler.add_job(
            self.run,
            trigger=trigger,
            args=[client, backend, self.policy],
        )

        self.status = Status.RUNNING

        active_policies = get_metric("active_policies")
        if active_policies:
            active_policies.add(1, {"policy": self.name})

    def run(
        self, client: DiodeClient | DiodeDryRunClient, backend: Backend, policy: Policy
    ):
        """
        Run the custom backend code for the specified scope.

        Args:
        ----
            client: Diode client.
            backend: Backend class.
            policy: Policy configuration.

        """
        job = self._create_job()
        self._record_execution_metrics()

        exec_start_time = time.perf_counter()
        entity_count = 0
        try:
            entities = backend.run(self.name, policy)
            metadata = self._build_metadata(job)

            entity_count = len(entities)
            if entity_count == 0:
                logger.info(f"Policy {self.name}: No entities to ingest")
                self._update_job_status(job, JobStatus.COMPLETED, None, 0)
                return

            chunk_num = self._ingest_entities(client, entities, metadata)
            logger.info(f"Policy {self.name}: Successfully ingested {entity_count} entities in {chunk_num} chunks")

            self._handle_success(job, entity_count)
        except Exception as e:
            logger.error(f"Policy {self.name}: {e}")
            self._handle_failure(job, e, entity_count)

        self._record_latency_metric(exec_start_time)

    def _create_job(self):
        """Create a job if job_store is available."""
        if self.job_store:
            return self.job_store.create_job(self.name)
        return None

    def _record_execution_metrics(self):
        """Record policy execution metrics."""
        policy_executions = get_metric("policy_executions")
        if policy_executions:
            policy_executions.add(1, {"policy": self.name})

    def _build_metadata(self, job):
        """Build metadata dictionary for ingestion."""
        metadata = {
            "policy_name": self.name,
            "worker_backend": self.metadata.name,
        }
        if job:
            metadata["job_id"] = job.id
        return metadata

    def _update_job_status(self, job, status: JobStatus, reason, entity_count: int):
        """Update job status if job_store and job are available."""
        if self.job_store and job:
            self.job_store.update_job(self.name, job.id, status, reason=reason, entity_count=entity_count)

    def _ingest_entities(
        self, client: DiodeClient | DiodeDryRunClient, entities: list[ingester_pb2.Entity], metadata: dict
    ) -> int:
        """Ingest entities in chunks and return the number of chunks."""
        chunk_num = 0
        for chunk_num, entity_chunk in enumerate(self._create_message_chunks(entities), 1):
            chunk_size_mb = self._estimate_message_size(entity_chunk) / (1024 * 1024)
            logger.debug(
                f"Ingesting chunk {chunk_num} with {len(entity_chunk)} entities (~{chunk_size_mb:.2f} MB)"
            )
            response = client.ingest(entities=entity_chunk, metadata=metadata)
            if response.errors:
                raise RuntimeError(f"Chunk {chunk_num} ingestion failed: {response.errors}")
            logger.debug(f"Chunk {chunk_num} ingested successfully")
        return chunk_num

    def _handle_success(self, job, entity_count: int):
        """Handle successful execution by updating job status and recording success metrics."""
        self._update_job_status(job, JobStatus.COMPLETED, None, entity_count)

        run_success = get_metric("backend_execution_success")
        if run_success:
            run_success.add(
                1,
                {
                    "policy": self.name,
                    "backend": self.metadata.name,
                    "app_name": self.metadata.app_name,
                    "app_version": self.metadata.app_version,
                },
            )

    def _handle_failure(self, job, error: Exception, entity_count: int):
        """Handle failed execution by updating job status and recording failure metrics."""
        self._update_job_status(job, JobStatus.FAILED, error, entity_count)

        run_failure = get_metric("backend_execution_failure")
        if run_failure:
            run_failure.add(
                1,
                {
                    "policy": self.name,
                    "backend": self.metadata.name,
                    "app_name": self.metadata.app_name,
                    "app_version": self.metadata.app_version,
                },
            )

    def _record_latency_metric(self, exec_start_time: float):
        """Record backend execution latency metric."""
        backend_execution_latency = get_metric("backend_execution_latency")
        if backend_execution_latency:
            exec_duration = (time.perf_counter() - exec_start_time) * 1000
            backend_execution_latency.record(
                exec_duration,
                {
                    "policy": self.name,
                    "backend": self.metadata.name,
                    "app_name": self.metadata.app_name,
                    "app_version": self.metadata.app_version,
                },
            )

    def stop(self):
        """Stop the policy runner."""
        self.scheduler.shutdown()
        self.status = Status.FINISHED
        active_policies = get_metric("active_policies")
        if active_policies:
            active_policies.add(-1, {"policy": self.name})

    def _create_message_chunks(self, entities: list[ingester_pb2.Entity]) -> list[list[ingester_pb2.Entity]]:
        """Create 3.5MB chunks from entities, always returning at least one chunk."""
        total_entities = len(entities)
        if total_entities == 0:
            return [entities]

        # Estimate total size and calculate approximate entities per chunk
        total_size = self._estimate_message_size(entities)
        target_bytes = TARGET_CHUNK_SIZE * 1024 * 1024

        if total_size <= target_bytes:
            # Single chunk if within limit
            return [entities]

        # Calculate entities per chunk based on size ratio
        entities_per_chunk = max(1, int(total_entities * target_bytes / total_size))

        chunks = []
        for i in range(0, total_entities, entities_per_chunk):
            chunk = entities[i : i + entities_per_chunk]
            chunks.append(chunk)

        return chunks


    def _estimate_message_size(self, entities: list[ingester_pb2.Entity]) -> int:
        """Estimate the serialized size of entities using minimal IngestRequest."""
        request = ingester_pb2.IngestRequest()
        request.entities.extend(entities)
        return request.ByteSize()
