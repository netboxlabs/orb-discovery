"""Several different loaders and dumpers for YAML under Python are implemented in the respective submodules."""

import sys

from . import ordereddict
from ._version import __version__

__author__ = 'Jonas Eschle "Mayou36", Johannes Lade "SebastianJL"'
__email__ = "jonas.eschle@phynix.science, johannes.lade@phynix.science"

__all__ = ["ordereddict"]

if sys.version_info[:2] < (3, 8):
    raise RuntimeError(
        "You are using Python < 3.8. This is not supported. "
        f"Please upgrade your Python version or use a version below 1.6 (current version: {__version__}."
    )
