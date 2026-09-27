"""Rebuilds the "## Example payloads" section of API_CONTRACT.md from ContractTests dumps, and
publishes the same dumps as docs/api/examples/<name>.json (the artifact the Go contract tests read).

Usage (from frontend/):
  TEST_RUNNER_SQ_DUMP_CONTRACT=<dump-dir> xcodebuild test … -only-testing:SideQuestzTests/ContractTests
  python3 scripts/gen_contract_examples.py [--examples-dir DIR] [--no-copy] <dump-dir> API_CONTRACT.md

Everything above "## Example payloads" is kept as written. Every example in GROUPS must have been
dumped (a missing one fails the run); dumps not in GROUPS are reported and not published. The examples
directory (default <repo>/docs/api/examples) is rewritten to hold exactly the listed examples plus
index.json, each normalized the same way (sorted keys, 2-space indent, UTF-8, trailing newline), so a
regeneration only changes what the models changed. --no-copy updates API_CONTRACT.md alone.
"""
import argparse
import json
import os
import sys

GROUPS = [
    ("Auth", [("SignupRequest", ""), ("AuthResponse", ""), ("UserPatch", "")]),
    ("Me", [("User", ""), ("UserHomeBase", "GET /me for an account with a home base (the demo account)"),
            ("Preferences", ""), ("TasteProfile", ""), ("Integrations", ""), ("PaymentMethods", "")]),
    ("Facebook (Graph API)", [("FacebookConnection", "GET /integrations/facebook"),
                              ("FacebookImport", "POST /integrations/facebook/import")]),
    ("Calendar + events", [("CalendarDays", ""), ("ItineraryItem", ""), ("TransitOptions", ""),
                           ("SearchResults", "GET /search?q=krog")]),
    ("Planning", [("ActivityHits", "GET /activities/search: an event, a free place, a place with no known price or distance (no near)"),
                  ("PlanRequest", ""),
                  ("PlanRequest.mustInclude", "with two must-see picks: every option includes them"),
                  ("PlanBatch", "the demo planner: required keys only"),
                  ("PlanBatch.dag", "the DAG planner: options with late_flag / total_cost_cents, stops with their extras"),
                  ("PlanBatch.empty", "no options, with the reason"),
                  ("RouteRequest", ""), ("RouteResult", ""),
                  ("RouteResult.dag", "an order that reaches a fixed start too late: broken_at, minutes_late"),
                  ("PlanAlternatives", "POST /plans/alternatives"),
                  ("ActivityDetail", "GET /activities/{id} for a place: its hours on the plan's day, rating and links"),
                  ("ActivityDetail.event", "an event: its start, end, venue and price"),
                  ("CreateItineraryRequest", "")]),
    ("Itineraries + ratings", [("Itinerary", ""), ("ItineraryUpdate", "PATCH body"), ("PastEvents", ""),
                               ("PastInsights", "GET /me/insights"), ("Rating", ""), ("Ticket", "on a booked item")]),
    ("Checkout", [("CheckoutIntent", "")]),
    ("Agentic checkout", [("CheckoutPlan", "GET /itineraries/{id}/checkout"),
                          ("CreateCheckoutRun", "POST /itineraries/{id}/checkout-runs"),
                          ("CheckoutRun", "GET /checkout/runs/{id}: one stop booked, the other over what was left of the budget")]),
    ("Forum", [("ForumQuery", "app side of GET /forum/posts; sent as query parameters"), ("ForumPosts", ""),
               ("MyFreePost", ""), ("NewFreePost", "app side of POST /forum/posts"), ("JoinResult", "")]),
    ("Threads, album, splits", [("ChatThread", ""), ("Messages", ""), ("Message", "with client_id"),
                                ("GroupPhotos", ""), ("NewExpense", ""), ("GroupLedger", "")]),
    ("Friends", [("Friends", ""), ("FriendRequests", ""),
                 ("OutgoingFriendRequest", "response of POST /friends/requests"), ("UserSearchResults", "")]),
]

INDEX_NOTE = "frontend ContractTests dump (TEST_RUNNER_SQ_DUMP_CONTRACT) via scripts/gen_contract_examples.py"


def normalized(value):
    """One JSON text for a value, whoever produced it (the app's encoder or this script)."""
    return json.dumps(value, indent=2, sort_keys=True, ensure_ascii=False) + "\n"


def load(path):
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def write(path, text):
    with open(path, "w", encoding="utf-8") as f:
        f.write(text)


def main():
    repo = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("dump_dir", help="where ContractTests wrote <name>.json")
    parser.add_argument("contract", help="frontend/API_CONTRACT.md")
    parser.add_argument("--examples-dir", default=os.path.join(repo, "docs", "api", "examples"),
                        help="where the normalized examples and index.json go (default: %(default)s)")
    parser.add_argument("--no-copy", action="store_true", help="only rewrite the contract's example section")
    args = parser.parse_args()

    names = [name for _, items in GROUPS for name, _ in items]
    missing = [name for name in names if not os.path.exists(os.path.join(args.dump_dir, name + ".json"))]
    dumped = {f[:-5] for f in os.listdir(args.dump_dir) if f.endswith(".json")}
    unused = sorted(dumped - set(names))
    if missing:
        print(f"missing dumps: {missing} (run ContractTests with TEST_RUNNER_SQ_DUMP_CONTRACT={args.dump_dir})", file=sys.stderr)
        sys.exit(1)

    text = open(args.contract, encoding="utf-8").read()
    marker = "## Example payloads"
    head = text[: text.index(marker)]
    out = [head, marker, "\n\nGenerated by `ContractTests` from the demo data (`TEST_RUNNER_SQ_DUMP_CONTRACT=<dir>`, then\n"
           "`python3 scripts/gen_contract_examples.py <dir> API_CONTRACT.md`). The same payloads live in\n"
           "[`docs/api/examples/`](../docs/api/examples/) for the backend's contract tests.\n"]
    for section, items in GROUPS:
        out.append(f"\n### {section}\n")
        for name, note in items:
            body = normalized(load(os.path.join(args.dump_dir, name + ".json"))).rstrip("\n")
            label = f"<code>{name}</code>" + (f" ({note})" if note else "")
            out.append(f"\n<details><summary>{label}</summary>\n\n```json\n{body}\n```\n</details>\n")
    write(args.contract, "".join(out))

    if not args.no_copy:
        os.makedirs(args.examples_dir, exist_ok=True)
        for name in names:
            write(os.path.join(args.examples_dir, name + ".json"), normalized(load(os.path.join(args.dump_dir, name + ".json"))))
        index = {"generated_by": INDEX_NOTE, "examples": sorted(names)}
        write(os.path.join(args.examples_dir, "index.json"), json.dumps(index, indent=2, ensure_ascii=False) + "\n")
        keep = {name + ".json" for name in names} | {"index.json"}
        for stale in sorted(set(os.listdir(args.examples_dir)) - keep):
            if stale.endswith(".json"):
                os.remove(os.path.join(args.examples_dir, stale))
                print("removed:", stale)
        print(f"wrote {len(names)} examples + index.json to {args.examples_dir}")
    print("not shown:", unused)


if __name__ == "__main__":
    main()
