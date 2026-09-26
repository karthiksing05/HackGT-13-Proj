"""HTTP API for the ML stack. Run `cd ml && uvicorn api.main:app`.

Layout: `routes/` translate HTTP to calls, `schemas/` define the wire
format, and `helpers/` do the computation, importing from the rest of the
ML stack (`compatibility`, ...). Nothing outside `api/` depends on FastAPI.
"""

from .app import create_app

__all__ = ["create_app"]
