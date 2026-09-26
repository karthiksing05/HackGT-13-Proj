"""Errors raised by helpers and mapped to HTTP status codes in `app.create_app`."""


class RankingRequestError(ValueError):
    """The request is well-formed but cannot be ranked (maps to HTTP 400)."""


class InferenceError(RuntimeError):
    """The compatibility model failed (maps to HTTP 500)."""
