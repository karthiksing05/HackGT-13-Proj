from .base import Adapter
from .google_places import GooglePlacesAdapter
from .osm_trails import OsmTrailsAdapter
from .resident_advisor import ResidentAdvisorAdapter
from .ticketmaster import TicketmasterAdapter

ADAPTERS: dict[str, type[Adapter]] = {
    a.name: a for a in (TicketmasterAdapter, GooglePlacesAdapter, ResidentAdvisorAdapter, OsmTrailsAdapter)
}
