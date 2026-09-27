# SideQuests

**Turn waiting into wandering.** Tell SideQuests where you are, when you need to be back and what you're in the mood for. It builds a timed plan of nearby events and places, with the travel between them, that fits your taste. The plan gets better every time you rate an outing, and you can open it up for people nearby to join.

---

## Inspiration

Three of us (Bayan, Kevin and Karthik) met on a study-abroad program this summer, and Max has plenty of his own travel stories. We kept running into the same problem: **free time in a place we didn't know, and no fast way to turn it into something worth doing.**

- **Bayan in Belgium:** they asked what to do and the internet said "eat waffles." They found some overpriced, mediocre waffles, saw a couple of tourist attractions a local suggested, then went straight back to the train station because nothing else came to mind.
- **Max's layovers:** in France, he and his mom planned a transit-only day with ChatGPT and got a generic list with no timing or routes. In Vienna, he and his dad wanted schnitzel and sightseeing. They took badly ordered routes, reached the sights after dark, and found every kitchen closed.
- **Karthik in Berlin:** he found that he loves travel when it fits him: nature and small groups in the Tiergarten, plus the city's social events. A generic "top 10" list would never have found that mix.
- **All summer:** we organized trips across a travel app, a calendar, a group chat and two payment apps. There had to be a better way.

Google Maps, ChatGPT, Fever, Wanderlog and meetup apps like WashedUp each cover one piece. **None of them start from your free window.** SideQuests turns events, places and transit into one timed plan, learns from your ratings, and lets others join until the plan locks.

## What it does

- **Taste onboarding:** you rate ten interests from 1 to 5, set your company, pace and spend, and answer three short prompts. You can also import your likes from Facebook, which the app turns into suggested ratings for you to approve. From all of this we build a likes embedding and a dislikes embedding.
- **A sidequest in about 30 seconds:** pick where you start and end, how far you'll go, and when you need to be back, then type or dictate your mood ("chill and outside, then live music"). In **0.2–0.6 s** you get three timed plans with the walk, transit or rideshare legs between stops. Drag to reorder and the route re-times itself. Press and hold a stop to swap it for something similar.
- **Social:** keep a plan private, share it with friends, or post it to the Forum with a group-size cap and a lock time. You can also post that you're free right now. Every group gets a chat, a shared photo album and an expense split that's exact to the cent. Updates arrive live over WebSockets.
- **Agentic checkout:** for ticketed stops, an agent finds tickets on the official site, fills in your details, waits for your approval (or buys instantly under a limit you set) and books them. The ticket then appears on that stop for everyone in the group.
- **The feedback loop:** you rate each stop after the outing, and those ratings update your taste profile, so your next plan starts smarter.

## How we built it

```
SwiftUI app ──HTTPS/WSS──► Cloudflare ► nginx ► Go API ──► MongoDB (catalog + app data)
                                                  │
                                                  └──► FastAPI ML service ──► Qwen3 embeddings
                                                       classifier · Jev rerank   (Vertex → HF → local)
Offline: ingestion crawler (Ticketmaster, Google Places, OSM, RA → Muse → Gemini) → MongoDB
         MPCDF Raven (4× A100): synthetic data + training → Hugging Face Hub → ML service
```

| Layer | Stack |
|---|---|
| iOS | Swift, SwiftUI, WebSockets, Keychain, Apple Speech, ASWebAuthenticationSession, XCTest/XCUITest |
| Backend | Go, MongoDB (2dsphere, TTL), JWT with rotating refresh tokens, a realtime hub, a checkout agent |
| ML | FastAPI, PyTorch, sentence-transformers, Qwen3-Embedding-0.6B, Qwen3.5-9B, Jev (TypeSafe), Vertex AI, Hugging Face, W&B |
| Data | Ticketmaster, Google Places, OpenStreetMap/OpenTopoData, Resident Advisor, Muse Spark (Meta), Gemini |
| Infra | Vultr VPS, nginx, Cloudflare, systemd, MPCDF Raven (SLURM, vLLM) |

### ML

The full details, including the data generation, architecture, ablations and usage code, are in our public model and dataset cards:
- **Model:** [karthiksing05/sidequestz-compatibility-classifier](https://huggingface.co/karthiksing05/sidequestz-compatibility-classifier)
- **Dataset:** [karthiksing05/sidequestz-event-embedding-text](https://huggingface.co/datasets/karthiksing05/sidequestz-event-embedding-text)
- **Training runs:** [W&B report](https://wandb.ai/karthiksing05-Independent/sidequestz-compatibility/reports/SideQuestz-Compatibility-Classifier-Training-and-Results--VmlldzoxODAxMDEwMQ)

In short:
- **One shared format.** Users, moods and events are all written in the same eight-section text (Interests, Activities, Social, Environment, Pace, Cost, Timing, Experience), then embedded with Qwen3-Embedding-0.6B. Dislikes are written as topics ("loud nightclub setting"), not as negations, because an embedding puts "I hate clubs" right next to "clubs."
- **Synthetic data at scale.** On the Raven supercomputer we generated **100k noisy event listings** and **10k personas**, then had an LLM judge rate **200k (user, event) pairs**. Users and events are split so that no test user or test event appears in training.
- **The compatibility classifier.** A 1.85M-parameter late-fusion network over the likes, dislikes and event embeddings. On unseen users and events it scores **NDCG@10 0.897 vs 0.849** and **Spearman 0.70 vs 0.50** against a tuned cosine baseline. It scores 200 events in about 6 ms on CPU.
- **Long-term taste plus the mood of the moment.** Each request blends the stored likes vector with the current mood (`0.4·likes + 0.6·mood`) and penalizes matches to the dislikes vector. The classifier scores the shortlist, and Jev reranks the top 20 with an LLM rubric. Each rating then moves your profile (`0.8·old + 0.2·activity`).
- **Reliable serving.** Vertex AI serves embeddings, with Hugging Face and a local Qwen model as fallbacks. Each provider must pass a parity gate (cosine ≥ 0.995 against golden vectors) before it serves traffic. We embedded 9,000+ real Atlanta activities in about 3 GPU-minutes.

### The planner

Ranking alone can't fix Max's Vienna layover. The planner turns ranked candidates into plans you can actually do:
- **Retrieval:** Mongo geo and time queries find candidates, then Go checks each one against opening hours, budget, travel range, age rules and the exclusions in your mood ("no bars").
- **The DAG:** each possible visit at a specific time is a node, and an edge means you can get from one visit to the next in time. The solver keeps the K best paths through this DAG, with one stop per category and no repeated venue or series.
- **Self-correcting rounds:** each round diagnoses the best plans for idle gaps, weak stops, uncovered interests or too few stops. It then fetches targeted candidates and solves again, for up to 3 rounds.
- **Guarantees:** every plan is inside your window, reachable, within budget and age-appropriate. The output is deterministic, and every run is logged for auditing.

### Data, backend and app

- **A self-sustaining catalog:** a crawler pulls new events hourly and places weekly, then deduplicates and merges them across sources. **Muse Spark** researches each activity with web search, **Gemini** writes its embedding text and description, and a timer embeds anything new. Past events expire automatically.
- **Backend:** the app's Swift models define the API. We generate JSON examples from them, and Go's contract tests decode every example strictly, which let four people build four subsystems in parallel.
- **App:** a custom design system, skeleton loaders, motion and live updates. It holds no business logic: everything is computed on the server. In total that's about 100k lines across Swift, Go and Python, with 147 test files.

## Sponsor tracks

- **AI/ML:** a trained and evaluated recommender, a supercomputer-scale synthetic data pipeline, LLM reranking, and a planner that uses ML scores and corrects its own plans.
- **Meta:** Muse Spark grounds every catalog activity with web research and powers ticket reservation. Facebook Login and the Graph API solve cold start by turning your likes into suggested taste ratings.
- **Visa:** agentic checkout. A Muse agent reserves tickets on the official site and Stripe processes payment on Visa rails. You approve each purchase or set an instant-checkout limit, and booked tickets sync to everyone on the plan.
- **MongoDB:** one database for everything: geo and time queries, TTL expiry, stored vectors, per-user catalogs, and planner logs that make every recommendation explainable.
- **Gemini:** writes the embedding text and description for every activity from Muse's research.
- **Vultr:** the whole production stack (API, ML inference, MongoDB, WebSockets) runs on one Vultr VPS.

## Challenges we ran into

- **Three contracts.** The app, the backend and the ML work were built separately and didn't agree on formats. We made the app the source of truth and enforced it with generated examples and strict contract tests.
- **No users, no labels.** We generated realistic noisy data and judged it with an LLM, using disjoint splits so the metrics mean something.
- **Embedding drift.** A single instruction prefix would have silently broken the classifier, so we built the parity gate.
- **Boring plans.** Early plans had one or two stops. Letting people join events late, adding a quality bar for stops and running the diagnosis rounds raised them to three or four.
- **Time.** Time zones, windows that run past midnight, and a demo that works on any day.

## Accomplishments that we're proud of

- A real model that beats a tuned baseline on unseen users and events, published with its data.
- Plans with hard guarantees in under a second.
- A live, deployed system with a real 9,000-activity catalog and a seeded demo world.
- An app that feels like a product.

## What we learned

- Putting everything in one text format mattered more than model architecture.
- Retrieval, ranking and planning are three different problems, and you need all three.
- Synthetic data needs as much rigor as real data.
- Log every decision the model makes, because those logs are tomorrow's training data.

## What's next for SideQuests

- **A travel ecosystem that feeds travelers:** travel agencies, tour operators and venues publish bookable experiences, and travelers and locals rate places, recommend spots and share their sidequests. Every contribution also trains the recommender, so the local's advice Bayan needed in Belgium is already in the app.
- **Retrain on real behavior.** Every shortlist, saved plan and rating is already logged.
- **Real routing and transit,** plus live delay alerts.
- **Calendar sync,** so SideQuests spots your free windows before you ask.
- **More cities.** Each one takes a config file and a crawl.

## Team

- **Karthik Singaravadivelan:** iOS frontend, integration, ML planning, Muse / Meta integration
- **Max Iliev:** backend, server setup
- **Kevin Harvey:** data acquisition and aggregation
- **Bayan Mardon:** ML stack, Visa integration

## Try it

Sign in as the demo account **Sandy Byte** (`demo@sidequestz.tech`) in the fictional city of Saltlight Harbor, or create an account to plan against the real Atlanta catalog.

## Built with

swift · swiftui · go · mongodb · python · fastapi · pytorch · qwen · hugging-face · vllm · weights-and-biases · vertex-ai · gemini · meta-muse · facebook-graph-api · stripe · visa · typesafe-jev · ticketmaster-api · google-places-api · openstreetmap · vultr · nginx · cloudflare · websockets
