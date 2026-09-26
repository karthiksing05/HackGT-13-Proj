import unittest
from pathlib import Path
from unittest import mock

from api.checkpoints import DEFAULT_CHECKPOINT, resolve_checkpoint


class ResolveCheckpointTests(unittest.TestCase):
    def test_local_path_is_returned_unchanged(self):
        with mock.patch("huggingface_hub.hf_hub_download") as download:
            self.assertEqual(resolve_checkpoint("runs/x/best.pt"), Path("runs/x/best.pt"))
        download.assert_not_called()

    def test_hf_reference_downloads_file_from_repo(self):
        with mock.patch("huggingface_hub.hf_hub_download", return_value="/cache/best.pt") as download:
            path = resolve_checkpoint(DEFAULT_CHECKPOINT, token="hf_test")
        self.assertEqual(path, Path("/cache/best.pt"))
        download.assert_called_once_with(
            "karthiksing05/sidequestz-compatibility-classifier", "final/best.pt", token="hf_test"
        )

    def test_nested_file_in_repo(self):
        with mock.patch("huggingface_hub.hf_hub_download", return_value="/cache/best.pt") as download:
            resolve_checkpoint("hf://owner/repo/runs/p1-flat-s1/best.pt")
        download.assert_called_once_with("owner/repo", "runs/p1-flat-s1/best.pt", token=None)

    def test_malformed_hf_reference_is_rejected(self):
        for reference in ("hf://", "hf://owner", "hf://owner/repo", "hf://owner/repo/"):
            with self.subTest(reference=reference), self.assertRaises(ValueError):
                resolve_checkpoint(reference)


if __name__ == "__main__":
    unittest.main()
