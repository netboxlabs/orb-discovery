#!/usr/bin/env python
# Copyright 2025 NetBox Labs Inc
"""Orb Worker Policy Runner."""

import inspect
import logging
import time
from dataclasses import dataclass
from datetime import datetime, timedelta

from apscheduler.schedulers.background import BackgroundScheduler
from apscheduler.triggers.cron import CronTrigger
from apscheduler.triggers.date import DateTrigger
from netboxlabs.diode.sdk import (
    DiodeClient,
    DiodeDryRunClient,
    DiodeOTLPClient,
    create_message_chunks,
)

from worker.backend import Backend, load_class
from worker.entity_metadata import apply_run_id_to_entities
from worker.exceptions import IngestError, IngestRejected
from worker.metrics import get_metric
from worker.models import DiodeConfig, Metadata, Policy, Status
from worker.package_finder import maybe_evict
from worker.policy.run import RunStatus, RunStore

logger = logging.getLogger(__name__)


@dataclass
class _RunOutcome:
    """What _execute_run reports back to its caller."""

    entity_count: int
    produce_seconds: float


class PolicyRunner:
    """Policy Runner class."""

    def __init__(self):
        """Initialize the PolicyRunner."""
        self.name = ""
        self.metadata = None
        self.policy = None
        self.status = Status.NEW
        self.scheduler = BackgroundScheduler()
        self.run_store = None
        self._diode_client = None

    def setup(
        self, name: str, diode_config: DiodeConfig, policy: Policy, run_store: RunStore
    ):
        """
        Set up the policy runner.

        Args:
        ----
            name: Policy name.
            diode_config: Diode configuration data.
            policy: Policy configuration data.
            run_store: RunStore instance for tracking runs.

        """
        self.name = name.replace("\r\n", "").replace("\n", "")
        policy.config.package = policy.config.package.replace("\r\n", "").replace(
            "\n", ""
        )

        # Evict stale cached modules if the bundle's `current` symlink has been
        # updated to a newer version since we last imported this package.
        maybe_evict(policy.config.package)

        # Debug logging for backend loading
        logger.debug(f"Loading backend class: {policy.config.package}")
        backend_class = load_class(policy.config.package)
        logger.debug(f"Backend class loaded successfully: {backend_class.__name__}")

        # Read the backend's identity WITHOUT committing to an instance, so the
        # ingest callback can be built and passed at construction time below.
        metadata = self._backend_metadata(backend_class)
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
        elif (
            diode_config.client_id is not None
            and diode_config.client_secret is not None
        ):
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
        self.run_store = run_store
        self._diode_client = client

        # Every dependency the closure reads (run_store, metadata, _diode_client)
        # is now assigned, so the callback is built first and the backend is
        # constructed ONCE with it — no post-construction attach step.
        backend = backend_class(ingest_callback=self._build_ingest_callback())

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

    def _backend_metadata(self, backend_class) -> Metadata:
        """
        Read the backend's metadata without committing to an instance.

        Prefer the class-level describe(); fall back to a throwaway instance's
        setup() for integrations that only implement the legacy instance method.
        """
        try:
            return backend_class.describe()
        except NotImplementedError:
            # Legacy backend: construct a throwaway just to read its metadata. The
            # real, callback-bearing instance is constructed below.
            logger.warning(
                "%s does not implement describe(); reading metadata via the "
                "deprecated setup() fallback (scheduled for removal in worker "
                "v2.0) — implement the describe() classmethod.",
                backend_class.__name__,
            )
            return backend_class().setup()

    def _build_ingest_callback(self):
        """
        Build a closure used to ingest entities outside the scheduled run() cycle.

        The returned callable signature:
            cb(entities=None, *, error=None, **kwargs) -> None

        Exactly one of ``entities`` / ``error`` must be supplied.
        On the ``entities`` path: a pseudo-run is created in the RunStore,
        entities are chunked and ingested via the same path run() uses, and
        response/transport errors are translated into IngestRejected /
        IngestError. On the ``error`` path: a failed pseudo-run is
        recorded; no client.ingest call is made; returns None.
        """

        def ingest_callback(
            entities=None,
            *,
            error: Exception | None = None,
            **kwargs,
        ) -> None:
            # kwargs is reserved for forward-compat (run_id, source, etc.); currently ignored.
            if (entities is None) == (error is None):
                raise TypeError(
                    "ingest_callback requires exactly one of 'entities' or 'error'"
                )
            if error is not None:
                run = self.run_store.create_run(
                    policy_name=self.name,
                    metadata={
                        "name": self.metadata.name,
                        "app_name": self.metadata.app_name,
                        "app_version": self.metadata.app_version,
                        "source": "ingest_callback",
                    },
                )
                self.run_store.update_run(
                    policy_name=self.name,
                    run_id=run.id,
                    status=RunStatus.FAILED,
                    error=error,
                    entity_count=0,
                )
                return
            try:
                self._execute_run(
                    self._diode_client, lambda run_id: entities, source="ingest_callback"
                )
            except IngestError:
                raise
            except Exception as exc:
                raise IngestError(str(exc)) from exc

        return ingest_callback

    def _execute_run(
        self, client, produce_entities, *, source: str | None = None
    ) -> _RunOutcome:
        """
        Create a run, produce + ingest entities through it, record COMPLETED/FAILED.

        ``produce_entities`` is a callable receiving ``run_id`` (the id of the
        run created here), invoked INSIDE the run's try-block — after
        ``create_run`` — so a failure while producing the entities (e.g. the
        backend's ``run()`` raising) is still recorded as a FAILED run rather
        than vanishing before the run is created. Re-raises on failure.

        The returned ``_RunOutcome.produce_seconds`` covers the producer call
        only — not chunking, ingest, or run-store writes — so callers can
        report truthful production timing.
        """
        run_metadata = {
            "name": self.metadata.name,
            "app_name": self.metadata.app_name,
            "app_version": self.metadata.app_version,
        }
        if source is not None:
            run_metadata["source"] = source
        run = self.run_store.create_run(policy_name=self.name, metadata=run_metadata)
        entity_count = 0
        try:
            produce_start = time.perf_counter()
            entities_list = list(produce_entities(run_id=run.id))
            produce_seconds = time.perf_counter() - produce_start
            entity_count = len(entities_list)
            apply_run_id_to_entities(entities_list, run.id)
            metadata = {
                "policy_name": self.name,
                "worker_backend": self.metadata.name,
                "run_id": run.id,
            }
            self._send_entities(client, entities_list, metadata)
            self.run_store.update_run(
                policy_name=self.name,
                run_id=run.id,
                status=RunStatus.COMPLETED,
                error=None,
                entity_count=entity_count,
            )
            return _RunOutcome(entity_count, produce_seconds)
        except Exception as exc:
            self.run_store.update_run(
                policy_name=self.name,
                run_id=run.id,
                status=RunStatus.FAILED,
                error=exc,
                entity_count=entity_count,
            )
            raise

    def _send_entities(self, client, entities_list: list, metadata: dict) -> int:
        """
        Send entities to the Diode client.

        Delegates chunking to the SDK's ``create_message_chunks``, which owns
        the gRPC message-size threshold (3 MB default, a safe margin below the
        4 MB ceiling) and returns a single chunk when the payload already fits.

        Returns the number of chunks actually sent (1 if not chunked).
        """
        chunks = create_message_chunks(entities_list)
        for chunk in chunks:
            response = client.ingest(entities=chunk, metadata=metadata)
            if response.errors:
                raise IngestRejected(f"Chunk ingestion failed: {response.errors}")
        return len(chunks)

    def run(
        self,
        client: DiodeClient | DiodeDryRunClient | DiodeOTLPClient,
        backend: Backend,
        policy: Policy,
    ):
        """
        Run the custom backend code for the specified scope.

        Args:
        ----
            client: Diode client.
            backend: Backend class.
            policy: Policy configuration.

        """
        policy_executions = get_metric("policy_executions")
        if policy_executions:
            policy_executions.add(1, {"policy": self.name})

        exec_start_time = time.perf_counter()
        try:
            logger.debug(f"Policy {self.name}: Starting backend execution")
            outcome = self._execute_run(
                client, self._build_entity_producer(backend, policy), source=None
            )
            logger.debug(
                f"Policy {self.name}: Backend execution completed in {outcome.produce_seconds:.3f} seconds"
            )
            logger.info(
                f"Policy {self.name}: Successfully ingested {outcome.entity_count} entities"
            )

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
        except Exception as e:
            logger.error(f"Policy {self.name}: {e}")

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

    def _build_entity_producer(self, backend: Backend, policy: Policy):
        """
        Bind ``backend.run`` for ``_execute_run``, passing per-tick context when accepted.

        Backends whose ``run()`` declares ``**kwargs`` receive
        ``source="scheduled"`` and ``run_id``; legacy two-argument signatures
        get the bare call (with a warning) so they keep working without a
        coordinated upgrade.
        """
        accepts_kwargs = any(
            parameter.kind is inspect.Parameter.VAR_KEYWORD
            for parameter in inspect.signature(backend.run).parameters.values()
        )
        if accepts_kwargs:
            return lambda run_id: backend.run(
                self.name, policy, source="scheduled", run_id=run_id
            )
        logger.warning(
            f"Policy {self.name}: backend run() does not declare **kwargs; "
            "per-tick context (source, run_id) will not be passed"
        )
        return lambda run_id: backend.run(self.name, policy)

    def stop(self):
        """Stop the policy runner."""
        self.scheduler.shutdown(wait=False)
        self.status = Status.FINISHED
        active_policies = get_metric("active_policies")
        if active_policies:
            active_policies.add(-1, {"policy": self.name})
