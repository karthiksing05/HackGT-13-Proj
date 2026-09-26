"""Operator tools, run as modules from `ml/`: `python -m tools.<name> --help`.

    hf_probe        does HF_TOKEN reach the Inference Providers router? (mapping + one embed per route)
    vertex_probe    does the service account reach the Vertex endpoint? (DNS, input key, dim, latency)
    make_golden     regenerate tests/fixtures: profile/search texts and local Qwen embeddings
    parity_check    cosine of a provider's (or a running service's) vectors against the local goldens
    embed_missing   embed activities without a current vector through the running service (systemd timer)
    rank_smoke      rank stored catalog vectors for a fixture profile through a running service, by eye
"""
