#!/usr/bin/env python
# Copyright 2025 NetBox Labs Inc
"""
Worker Package Finder.

Discovers plugins installed by the PackageManager from disk.
Bundles are extracted to BUNDLES_ROOT/<name>/current/ and this
finder makes them importable without pip.
"""

import importlib.abc
import importlib.util
import logging
import os
import sys
from pathlib import Path

logger = logging.getLogger(__name__)

# Root directory where the package manager extracts bundles:
#   BUNDLES_ROOT/<bundle_name>/current/  (symlink → <version>/)
BUNDLES_ROOT = Path(os.environ["BUNDLES_ROOT_PATH"])


class OrbPackageFinder(importlib.abc.MetaPathFinder):
    """
    sys.meta_path finder that resolves modules from bundle directories.

    Appended last in sys.meta_path so it only fires after stdlib and
    pip-installed packages are exhausted.
    """

    def find_spec(self, fullname: str, path, target=None):
        """Locate a module spec by scanning active bundle current/ directories."""
        top_level = fullname.split(".")[0]

        for bundle_dir in self._active_bundle_dirs():
            # Package directory (top_level/__init__.py)
            candidate = bundle_dir / top_level
            if (candidate / "__init__.py").is_file():
                spec = importlib.util.spec_from_file_location(
                    fullname,
                    candidate / "__init__.py",
                    submodule_search_locations=[str(candidate)],
                )
                if spec is not None:
                    logger.debug(f"PackageFinder: resolved '{fullname}' from {bundle_dir}")
                    return spec

            # Single-file module (top_level.py)
            module_file = bundle_dir / f"{top_level}.py"
            if module_file.is_file():
                spec = importlib.util.spec_from_file_location(fullname, module_file)
                if spec is not None:
                    logger.debug(f"PackageFinder: resolved '{fullname}' from {bundle_dir}")
                    return spec

        return None

    def _active_bundle_dirs(self) -> list[Path]:
        """Return current/ dirs for all bundles with a valid symlink."""
        if not BUNDLES_ROOT.is_dir():
            return []
        return [
            b / "current"
            for b in BUNDLES_ROOT.iterdir()
            if (b / "current").exists()
        ]


def _maybe_evict(package_name: str) -> None:
    """
    Evict a package from sys.modules if its bundle symlink has changed.

    Called in PolicyRunner.setup() before load_class() so a version upgrade
    by the PackageManager takes effect without restarting the worker.

    The resolved symlink path is stamped onto the module as __orb_bundle_path__
    here (post-import) rather than in find_spec (pre-import) so sys.modules
    is guaranteed to contain the module when we write the attribute.

    Args:
    ----
        package_name: Top-level module name (e.g. "nbl_custom_worker").

    """
    # Derive the bundle directory name: module names use underscores,
    # bundle dirs may use hyphens (e.g. nbl-custom-worker). Check both.
    bundles_root = BUNDLES_ROOT
    candidates = [
        bundles_root / package_name,
        bundles_root / package_name.replace("_", "-"),
    ]
    current = next(
        (c / "current" for c in candidates if (c / "current").exists()), None
    )
    if current is None:
        return

    try:
        resolved = str(current.resolve())
    except OSError:
        return

    mod = sys.modules.get(package_name)
    if mod is None:
        return

    # Stamp on first sight so we have a baseline for future calls.
    cached = getattr(mod, "__orb_bundle_path__", None)
    if cached is None:
        mod.__orb_bundle_path__ = resolved
        return

    if cached != resolved:
        to_remove = [
            k for k in sys.modules
            if k == package_name or k.startswith(f"{package_name}.")
        ]
        for key in to_remove:
            del sys.modules[key]
        logger.info(
            f"PackageFinder: evicted {len(to_remove)} module(s) for '{package_name}' "
            f"({cached!r} → {resolved!r})"
        )
    else:
        logger.debug(f"PackageFinder: '{package_name}' is current, no eviction needed")


def install_finder() -> None:
    """Install OrbPackageFinder into sys.meta_path (idempotent)."""
    if any(isinstance(f, OrbPackageFinder) for f in sys.meta_path):
        logger.debug("PackageFinder: already installed, skipping")
        return
    sys.meta_path.append(OrbPackageFinder())
    logger.info(f"PackageFinder: installed (bundles root: {BUNDLES_ROOT})")
