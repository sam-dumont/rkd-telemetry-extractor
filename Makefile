.PHONY: install test lint clean

install:
	pip install pytest pytest-cov

test:
	python -m pytest

lint:
	python -m py_compile rkd_parser.py

clean:
	rm -rf __pycache__ .pytest_cache .coverage htmlcov tests/__pycache__
