"""Entry point: `python -m severino_knowledge_router` and the console script."""

from .server import run


def main() -> None:
    run()


if __name__ == "__main__":
    main()
