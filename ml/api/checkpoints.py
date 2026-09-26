"""Resolve a classifier checkpoint reference to a local file.

A reference is either a local path or `hf://<owner>/<repo>/<file in repo>`,
which is downloaded from the Hugging Face Hub (and cached) with `HF_TOKEN`.

The default is the final model (`final/best.pt` from
karthiksing05/sidequestz-compatibility-classifier, weights and config only)
committed at `ml/checkpoints/`, so the server starts without network access.
"""

from pathlib import Path

HF_PREFIX = "hf://"
DEFAULT_CHECKPOINT = str(Path(__file__).resolve().parents[1] / "checkpoints" / "compatibility_classifier.pt")


def resolve_checkpoint(reference: str, token: str | None = None) -> Path:
    """Return a local path for `reference`, downloading `hf://` references first.

    `token` defaults to `HF_TOKEN` (or the `hf auth login` token), as in `hf_hub_download`.
    """
    if not reference.startswith(HF_PREFIX):
        return Path(reference)

    owner, _, rest = reference[len(HF_PREFIX):].partition("/")
    name, _, filename = rest.partition("/")
    if not (owner and name and filename):
        raise ValueError(f"expected hf://<owner>/<repo>/<file>, got {reference!r}")

    # Imported here so local-path deployments do not need huggingface_hub.
    from huggingface_hub import hf_hub_download

    return Path(hf_hub_download(f"{owner}/{name}", filename, token=token))
