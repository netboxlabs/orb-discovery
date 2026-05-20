#!/usr/bin/env python
# Copyright 2025 NetBox Labs Inc
"""Orb Worker Policy Runner."""

import inspect
import logging
import time
from datetime import datetime, timedelta

from apscheduler.schedulers.background import BackgroundScheduler
from apscheduler.triggers.cron import CronTrigger
from apscheduler.triggers.date import DateTrigger
from netboxlabs.diode.sdk import (
    DiodeClient,
    DiodeDryRunClient,
    DiodeOTLPClient,
    create_message_chunks,
    estimate_message_size,
)

from worker.backend import Backend, load_class
from worker.entity_metadata import apply_run_id_to_entities
from worker.exceptions import IngestError, IngestRejected, IngestUnavailable
from worker.metrics import get_metric
from worker.models import DiodeConfig, Policy, Status
from worker.package_finder import maybe_evict
from worker.policy.run import RunStatus, RunStore

# Diode message-size cap (per chunk). Stays in sync with the reconciler's
# 4 MiB gRPC ceiling minus a safety margin.
MAX_INGEST_MESSAGE_BYTES = 3 * 1024 * 1024

logger = logging.getLogger(__name__)


def _construct_backend(backend_class, *, ingest_callback):
    """
    Construct a Backend, passing ``ingest_callback`` only when accepted.

    Uses ``inspect.signature`` to detect whether ``backend_class.__init__``
    has a matching named parameter or ``VAR_KEYWORD`` absorber. Legacy
    backends with ``__init__(self)`` get constructed zero-arg and never
    see the kwarg, so the worker can ship this contract bump without a
    coordinated upgrade across every integration package.
    """
    sig = inspect.signature(backend_class)
    params = sig.parameters
    accepts_var_kw = any(
        p.kind is inspect.Parameter.VAR_KEYWORD for p in params.values()
    )
    init_kwargs: dict[str, object] = {}
    if accepts_var_kw or "ingest_callback" in params:
        init_kwargs["ingest_callback"] = ingest_callback
    return backend_class(**init_kwargs)


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
        self._callback_ready = False

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

        # Build the ingest callback closure. It captures `self` and reads
        # `self._diode_client` lazily, so it is safe to construct before the
        # client is assigned below.
        ingest_callback = self._build_ingest_callback(self.name)
        backend = _construct_backend(backend_class, ingest_callback=ingest_callback)
        logger.debug(f"Backend class loaded successfully: {backend_class.__name__}")

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

        # Callback is now safe to invoke — every dependency the closure reads
        # (run_store, metadata, _diode_client) is attached. Integrations may push
        # entities via ingest_callback from this point onward.
        self._callback_ready = True

        active_policies = get_metric("active_policies")
        if active_policies:
            active_policies.add(1, {"policy": self.name})

    def _build_ingest_callback(self, policy_name: str):
        """
        Build a closure used to ingest entities outside the scheduled run() cycle.

        The returned callable signature:
            cb(entities=None, *, error=None, **kwargs) -> None

        Exactly one of ``entities`` / ``error`` must be supplied.
        On the ``entities`` path: a pseudo-run is created in the RunStore,
        entities are chunked and ingested via the same path run() uses, and
        response/transport errors are translated into IngestRejected /
        IngestUnavailable. On the ``error`` path: a failed pseudo-run is
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
            if not self._callback_ready:
                raise IngestUnavailable(
                    "ingest_callback invoked before the worker finished constructing "
                    "this Backend (likely called from Backend.__init__ or "
                    "Backend.setup() — defer until after setup() returns)"
                )
            run = self.run_store.create_run(
                policy_name=policy_name,
                metadata={
                    "name": self.metadata.name,
                    "app_name": self.metadata.app_name,
                    "app_version": self.metadata.app_version,
                    "source": "ingest_callback",
                },
            )
            if error is not None:
                self.run_store.update_run(
                    policy_name=policy_name,
                    run_id=run.id,
                    status=RunStatus.FAILED,
                    error=error,
                    entity_count=0,
                )
                return
            entities_list: list = []
            try:
                entities_list = list(entities)
                apply_run_id_to_entities(entities_list, run.id)
                metadata = {
                    "policy_name": policy_name,
                    "worker_backend": self.metadata.name,
                    "run_id": run.id,
                }
                self._send_entities(self._diode_client, entities_list, metadata)
            except IngestError as exc:
                self.run_store.update_run(
                    policy_name=policy_name,
                    run_id=run.id,
                    status=RunStatus.FAILED,
                    error=exc,
                    entity_count=len(entities_list),
                )
                raise
            except Exception as exc:
                logger.exception(
                    "Unexpected exception in ingest_callback; "
                    "translating to IngestUnavailable"
                )
                self.run_store.update_run(
                    policy_name=policy_name,
                    run_id=run.id,
                    status=RunStatus.FAILED,
                    error=exc,
                    entity_count=len(entities_list),
                )
                raise IngestUnavailable(str(exc)) from exc
            self.run_store.update_run(
                policy_name=policy_name,
                run_id=run.id,
                status=RunStatus.COMPLETED,
                error=None,
                entity_count=len(entities_list),
            )

        return ingest_callback

    def _send_entities(self, client, entities_list: list, metadata: dict) -> int:
        """
        Send entities to the Diode client, chunking if the payload exceeds MAX_INGEST_MESSAGE_BYTES.

        Returns the number of chunks actually sent (1 if not chunked).
        """
        size_bytes = estimate_message_size(entities_list)
        if size_bytes > MAX_INGEST_MESSAGE_BYTES:
            chunks = create_message_chunks(entities_list)
            for chunk in chunks:
                response = client.ingest(entities=chunk, metadata=metadata)
                if response.errors:
                    raise IngestRejected(f"Chunk ingestion failed: {response.errors}")
            return len(chunks)
        response = client.ingest(entities=entities_list, metadata=metadata)
        if response.errors:
            raise IngestRejected(f"Entities ingestion failed: {response.errors}")
        return 1

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

        # CREATE RUN AT START with metadata from backend setup
        run_metadata = {
            "name": self.metadata.name,
            "app_name": self.metadata.app_name,
            "app_version": self.metadata.app_version,
        }
        run = self.run_store.create_run(
            policy_name=self.name,
            metadata=run_metadata,
        )

        exec_start_time = time.perf_counter()
        entity_count = 0
        try:
            logger.debug(f"Policy {self.name}: Starting backend execution")
            entities = list(backend.run(self.name, policy))
            elapsed = time.perf_counter() - exec_start_time
            logger.debug(f"Policy {self.name}: Backend execution completed in {elapsed:.3f} seconds")
            entity_count = len(entities)

            apply_run_id_to_entities(entities, run.id)

            metadata = {
                "policy_name": self.name,
                "worker_backend": self.metadata.name,
                "run_id": run.id,
            }
            chunk_num = self._send_entities(client, entities, metadata)
            logger.info(
                f"Policy {self.name}: Successfully ingested {entity_count} entities in {chunk_num} chunks"
            )

            # UPDATE RUN ON SUCCESS
            self.run_store.update_run(
                policy_name=self.name,
                run_id=run.id,
                status=RunStatus.COMPLETED,
                error=None,
                entity_count=entity_count,
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

            # UPDATE RUN ON FAILURE
            self.run_store.update_run(
                policy_name=self.name,
                run_id=run.id,
                status=RunStatus.FAILED,
                error=e,
                entity_count=entity_count,
            )

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

    def stop(self):
        """Stop the policy runner."""
        self._callback_ready = False
        self.scheduler.shutdown(wait=False)
        self.status = Status.FINISHED
        active_policies = get_metric("active_policies")
        if active_policies:
            active_policies.add(-1, {"policy": self.name})
