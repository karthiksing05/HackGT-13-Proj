"""Errors raised by helpers and mapped to HTTP status codes in `app.create_app`."""


class RankingRequestError(ValueError):
    """The request is well-formed but cannot be ranked (maps to HTTP 400)."""


class InferenceError(RuntimeError):
    """The compatibility model failed (maps to HTTP 500)."""


class EmbeddingUnavailableError(RuntimeError):
    """No embedding provider could serve the request (maps to HTTP 503)."""

    MESSAGE = "Embedding provider unavailable."

    def __init__(self, message: str = MESSAGE) -> None:
        super().__init__(message)
