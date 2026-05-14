"""Test configuration — set required environment variables before any imports."""
import os

os.environ.setdefault("BUNDLES_ROOT_PATH", "/tmp/orb-test-bundles")
