"""`GET /healthz`: state of the ranking model and the embedding providers.

`?probe=1` embeds the canary through the providers (bypassing the cache) first; when that fails the
full report comes back with status `degraded` and HTTP 503, so a deploy check can fail loudly and
an operator still sees which provider said what.
"""

from fastapi import APIRouter, Depends, Query
from fastapi.responses import JSONResponse

from ..deps import get_health
from ..errors import EmbeddingUnavailableError
from ..helpers.health import HealthState
from ..schemas.health import HealthResponse

router = APIRouter()


@router.get("/healthz", response_model=HealthResponse)
def healthz(probe: bool = Query(False), health: HealthState = Depends(get_health)):
    try:
        return health.snapshot(probe=probe)
    except EmbeddingUnavailableError:
        report = health.snapshot(probe=False).model_copy(update={"status": "degraded"})
        return JSONResponse(status_code=503, content=report.model_dump())
