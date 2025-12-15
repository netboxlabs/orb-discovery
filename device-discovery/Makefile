.PHONY: install-dev-tools
install-dev-tools:
	@python -m pip install --upgrade pip
	@pip install .
	@pip install .[dev]
	@pip install .[test]

.PHONY: test
test:
	@pytest --cov-report=term-missing:skip-covered --cov=device_discovery/

.PHONY: lint
lint:
	@ruff check --output-format=github device_discovery/ tests/

.PHONY: fix-lint
fix-lint:
	@ruff check --fix device_discovery/ tests/

.PHONY: format
format:
	@ruff format device_discovery/ tests/

