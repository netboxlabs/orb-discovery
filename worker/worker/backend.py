#!/usr/bin/env python
# Copyright 2025 NetBox Labs Inc
"""NetBox Labs - Orb Worker Backend."""

import importlib
import inspect
from collections.abc import Iterable

from netboxlabs.diode.sdk.ingester import Entity

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

        The worker constructs the Backend with no arguments and assigns
        ``ingest_callback`` afterwards, once its dependencies are ready — so
        ``__init__`` does not receive it from the current worker. The
        parameter and ``**kwargs`` stay accepted for forward-compatibility
        and for integrations that pass them directly.

        Args:
        ----
            ingest_callback: Optional callable that ingests entities or
                reports errors outside of the ``run()`` cycle. **Do not
                invoke from ``__init__`` or ``setup()`` — the callback is
                only usable starting after the worker finishes constructing
                the Backend (i.e. after ``setup()`` returns).** See
                ``worker.exceptions`` for the exception hierarchy it may
                raise.
            **kwargs: Forward-compat door for additional resources worker
                may pass in future versions; silently ignored by default.

        """
        self.ingest_callback = ingest_callback

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

    def setup(self) -> Metadata:
        """
        Set up the backend.

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
            **kwargs: Passive forward-compat door. The worker passes nothing
                through it in v1; future minor releases may add per-tick
                context (e.g. ``source="scheduled"|"trigger"``, ``run_id``).
                Concrete backends are encouraged to declare ``**kwargs`` so
                additive kwargs ride into the contract without a coordinated
                upgrade.

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
