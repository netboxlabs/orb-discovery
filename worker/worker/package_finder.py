#!/usr/bin/env python
# Copyright 2025 NetBox Labs Inc
"""Orb Worker Package Finder.

Discovers plugins installed by orb-agent's PackageManager from disk.
Bundles are extracted to /opt/orb/packages/<name>/current/ and this
finder makes them importable without pip.
"""

import importlib.abc
import importlib.machinery
import importlib.util
import logging
import os
import sys
from pathlib import Path

logger = logging.getLogger(__name__)

# Root directory where orb-agent extracts bundles.
# Matches the path in the OBS-2925 architecture:
#   /opt/orb/packages/<name>/current/  (symlink → <version>/)
BUNDLES_ROOT = Path(os.getenv("ORB_BUNDLES_ROOT", "/opt/orb/packages"))


class OrbPackageFinder(importlib.abc.MetaPathFinder):
    """
    sys.meta_path finder that discovers modules from orb-agent bundle directories.

    Each bundle is extracted by orb-agent's PackageManager to:
        BUNDLES_ROOT/<bundle_name>/current/

    where `current` is an atomic symlink pointing at the active version directory.
    This finder adds those directories to the module search path on demand so that
    `import <module>` resolves correctly without any pip involvement.
    """

    def find_spec(self, fullname: str, path, target=None):
        """
        Locate a module spec by scanning active bundle directories.

        Only the top-level package name is used for the directory scan;
        sub-module resolution is handled by the standard machinery once
        the top-level package path is registered.
        """
        top_level = fullname.split(".")[0]

        for bundle_dir in self._active_bundle_dirs():
            candidate = bundle_dir / top_level
            init = candidate / "__init__.py"

            if init.is_file():
                spec = importlib.util.spec_from_file_location(
                    fullname,
                    init,
                    submodule_search_locations=[str(candidate)],
                )
                if spec is not None:
                    # Stamp the resolved bundle path so _maybe_evict can detect upgrades.
                    self._stamp_bundle_path(fullname, bundle_dir)
                    logger.debug(
                        f"OrbPackageFinder: resolved '{fullname}' from {bundle_dir}"
                    )
                    return spec

            # Single-file module (e.g. top_level.py)
            module_file = bundle_dir / f"{top_level}.py"
            if module_file.is_file():
                spec = importlib.util.spec_from_file_location(fullname, module_file)
                if spec is not None:
                    self._stamp_bundle_path(fullname, bundle_dir)
                    logger.debug(
                        f"OrbPackageFinder: resolved '{fullname}' from {bundle_dir}"
                    )
                    return spec

        return None

    def _active_bundle_dirs(self) -> list[Path]:
        """
        Return the list of `current/` directories for all installed bundles.

        Skips bundles where the symlink is missing or broken.
        """
        if not BUNDLES_ROOT.is_dir():
            return []

        dirs = []
        for bundle in BUNDLES_ROOT.iterdir():
            current = bundle / "current"
            if current.exists():  # follows symlink; False if broken
                dirs.append(current)
        return dirs

    @staticmethod
    def _stamp_bundle_path(fullname: str, bundle_dir: Path) -> None:
        """
        Stamp __orb_bundle_path__ on the top-level module after import.

        This is the value _maybe_evict() compares against to detect
        whether orb-agent has swapped the symlink to a newer version.
        """
        top_level = fullname.split(".")[0]
        mod = sys.modules.get(top_level)
        if mod is not None:
            resolved = str((bundle_dir / "..").resolve() / "current")
            try:
                resolved = str((bundle_dir).resolve())
            except OSError:
                pass
            mod.__orb_bundle_path__ = resolved


def _maybe_evict(package_name: str) -> None:
    """
    Evict a package from sys.modules if its `current` symlink has changed.

    Called from PolicyRunner.setup() before loading a backend class so that
    a version upgrade applied by orb-agent's PackageManager takes effect
    without restarting the worker process.

    Args:
    ----
        package_name: The top-level module name (e.g. "nbl_cisco_meraki").

    """
    bundle_dir = BUNDLES_ROOT / package_name
    current = bundle_dir / "current"

    if not current.exists():
        # Bundle not managed by OrbPackageFinder; nothing to evict.
        return

    try:
        resolved = str(current.resolve())
    except OSError:
        return

    mod = sys.modules.get(package_name)
    if mod is None:
        # Not yet imported — nothing to evict.
        return

    cached_path = getattr(mod, "__orb_bundle_path__", None)
    if cached_path != resolved:
        to_remove = [
            k for k in sys.modules
            if k == package_name or k.startswith(f"{package_name}.")
        ]
        for key in to_remove:
            del sys.modules[key]
        logger.info(
            f"OrbPackageFinder: evicted {len(to_remove)} module(s) for '{package_name}' "
            f"(symlink: {cached_path!r} → {resolved!r})"
        )
    else:
        logger.debug(
            f"OrbPackageFinder: '{package_name}' is current, no eviction needed"
        )


def install_finder() -> None:
    """
    Install OrbPackageFinder into sys.meta_path if not already present.

    Safe to call multiple times (idempotent).
    Called once from worker/main.py at startup.
    """
    for finder in sys.meta_path:
        if isinstance(finder, OrbPackageFinder):
            logger.debug("OrbPackageFinder: already installed, skipping")
            return

    sys.meta_path.append(OrbPackageFinder())
    logger.info(f"OrbPackageFinder: installed (bundles root: {BUNDLES_ROOT})")
