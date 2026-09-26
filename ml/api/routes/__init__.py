from .embedding import router as embedding_router
from .health import router as health_router
from .profile import router as profile_router
from .ranking import router as ranking_router
from .user_embedding import router as user_embedding_router

__all__ = ["embedding_router", "health_router", "profile_router", "ranking_router", "user_embedding_router"]
