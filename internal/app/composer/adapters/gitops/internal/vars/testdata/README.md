# Public test fixtures

`age-key.txt` and `other-key.txt` are intentionally public, disposable age identities.
The encrypted YAML files contain synthetic test values, including `base-test-secret`,
`stage-test-secret`, and `updated-test-secret`. Tests cover decryption, multiple
recipients, tampering, and secret rotation. These identities must never encrypt
real data or be installed on a deployment host.
