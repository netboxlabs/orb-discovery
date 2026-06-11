#!/usr/bin/env python
# Copyright 2025 NetBox Labs Inc
"""NetBox Labs - Orb Worker Backend."""

import importlib
import inspect
import warnings
from collections.abc import Iterable

from netboxlabs.diode.sdk.ingester import Entity
from typing_extensions import deprecated

from worker.models import Metadata, Policy


class Backend:
    """Backend Class."""

    def __init__(
        self,
        *,
        ingest_callback=None,
        **kwargs,
    ) -> None:
        """
        Construct the Backend.

        The worker reads the backend's metadata first (via the ``describe()``
        classmethod, or a throwaway legacy ``setup()`` instance that receives
        no callback), then constructs the instance it will run with
        ``ingest_callback`` passed here. Every dependency the callback uses
        is ready by that point, so the callback is usable as soon as the
        instance exists — just do not invoke it from ``__init__`` itself.

        Args:
        ----
            ingest_callback: Optional callable that ingests entities or
                reports errors outside of the ``run()`` cycle. See
                ``worker.exceptions`` for the exception hierarchy it may
                raise.
            **kwargs: Forward-compat door for additional resources worker
                may pass in future versions; silently ignored by default.

        """
        self.ingest_callback = ingest_callback

    def __init_subclass__(cls, **kwargs) -> None:
        """Warn once, at class-definition time, when a subclass still relies on setup()."""
        super().__init_subclass__(**kwargs)
        overrides_setup = any(
            "setup" in klass.__dict__ for klass in cls.__mro__[:-1] if klass is not Backend
        )
        has_describe = cls.describe.__func__ is not Backend.describe.__func__
        if overrides_setup and not has_describe:
            warnings.warn(
                f"{cls.__module__}.{cls.__qualname__} overrides Backend.setup(), which is "
                "deprecated — implement the describe() classmethod instead "
                "(the setup() fallback will be removed in worker v2.0).",
                DeprecationWarning,
                stacklevel=2,
            )

    @classmethod
    def describe(cls) -> Metadata:
        """
        Return the backend's metadata without constructing an instance.

        Preferred over setup(): lets the worker read the backend's identity
        (name/app_name/app_version) before constructing it, so the ingest
        callback can be built and passed at construction time. Integrations
        that only implement the instance setup() are still supported — the
        worker falls back to a throwaway instance to read their metadata.
        """
        raise NotImplementedError("The 'describe' classmethod must be implemented.")

    @deprecated(
        "Implement the describe() classmethod instead; "
        "the setup() fallback will be removed in worker v2.0."
    )
    def setup(self) -> Metadata:
        """
        Set up the backend.

        .. deprecated::
            Implement the :meth:`describe` classmethod instead. The worker reads
            metadata via ``describe()`` and only falls back to a throwaway
            instance's ``setup()`` for legacy backends; that fallback is
            scheduled for removal in worker v2.0.

        Returns
        -------
            Metadata: The metadata for the backend.

        """
        raise NotImplementedError("The 'setup' method must be implemented.")

    def run(
        self,
        policy_name: str,
        policy: Policy,
        **kwargs,
    ) -> Iterable[Entity]:
        """
        Run the backend.

        Args:
        ----
            policy_name (str): The name of the policy.
            policy (Policy): The policy to run.
            **kwargs: Per-tick context from the worker. When the overriding
                signature declares ``**kwargs``, the worker passes
                ``source="scheduled"`` and ``run_id`` (the worker-side run
                identifier, also stamped on the produced entities' metadata).
                Legacy signatures without ``**kwargs`` keep working — the
                worker detects them and falls back to the bare two-argument
                call — but declare ``**kwargs`` so future additive context
                rides into the contract without a coordinated upgrade.

        Returns:
        -------
            Iterable[Entity]: The entities produced by the backend.

        """
        raise NotImplementedError("The 'run' method must be implemented.")


def load_class(module_name: str) -> type[Backend]:
    """
    Dynamically load a class from a given module and ensure it conforms to Backend.

    Args:
    ----
        module_name (str): The module name.

    """
    try:
        module = importlib.import_module(module_name)
        for _, obj in inspect.getmembers(module):
            if inspect.isclass(obj) and issubclass(obj, Backend):
                return obj
        raise ImportError("No class inheriting 'Backend'")
    except (ImportError, AttributeError) as e:
        raise RuntimeError(
            f"Failed to load a class inheriting from 'Backend' in module '{module_name}': {e}"
        )
