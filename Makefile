.PHONY: hooks

hooks:
	pip install pre-commit --break-system-packages
	pre-commit install --install-hooks
