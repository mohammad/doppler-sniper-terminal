# doppler-sniper

`doppler-sniper` is a read-only terminal tool for analyzing a single Doppler asset at a time and flagging likely sniper behavior from the public GraphQL indexer.

Today it works best for **Base `zora` assets**. That is the pool family where I have most consistently found raw wallet-level swaps in the public API.

## How It Works

Given an asset address, the app:

1. Resolves the asset to its canonical pool.
2. Checks whether raw chronological swaps are publicly available.
3. Runs a wallet-behavior heuristic on those swaps.
4. Shows a summary, detail view, leaderboard, and comparison view for that market.

Wallet labels:

- `SNIPER`: bought early and sold at least 50% of bought tokens
- `BELIEVER`: bought early and sold less than 20%
- `MIXED`: bought early and sold between 20% and 50%
- `LATE_BUYER`: first buy happened after the early cutoff

## Analyzable vs Unanalyzable

A market is analyzable when the public API exposes:

- a canonical pool
- raw `swaps`
- wallet addresses on those swaps

A market is not analyzable when the public API only exposes aggregate state like:

- `lastSwapTimestamp`
- pool metadata
- bucketed data such as `fifteenMinuteBucketUsds`

That aggregate data may be enough for charts, but not for wallet-level sniper detection. When that happens, the app refuses the market instead of guessing.

Practical rule:

> If you want this tool to work reliably today, start with Base `zora` assets.

## Early Cutoff

The early cutoff is not time-based. It is the timestamp where cumulative buy volume first reaches `20%` of `launch.TotalTokensSold`.

That means:

- “early” is supply-based, not “first N minutes”
- the percentage is based on reported sold launch supply, not total token supply
- if `TotalTokensSold` is missing, the app falls back to the last observed swap timestamp

## Large Markets

For large markets, the app uses streaming analysis:

- swaps are fetched page-by-page
- wallet totals are accumulated incrementally
- only a bounded recent tail is retained for the UI

For very large markets, the interactive path becomes partial on purpose:

- the app analyzes the first `20,000` chronological swaps
- labels the result as partial
- stays within an interactive timeout budget

## Run It

Requirements:

- Go 1.22+
- `rg` (ripgrep)

Example `.env`:

```env
INDEXER_GRAPHQL_URL=https://prod.indexer.doppler.lol/graphql
DOPPLER_CHAIN_ID=8453
```

Optional:

```env
DOPPLER_ASSET_ADDRESS=0x50f88fe97f72cd3e75b9eb4f747f59bceba80d59
```

Then:

```bash
cp .env.example .env
make ui
```

Or one-off:

```bash
INDEXER_GRAPHQL_URL=https://prod.indexer.doppler.lol/graphql \
DOPPLER_CHAIN_ID=8453 \
DOPPLER_ASSET_ADDRESS=0x50f88fe97f72cd3e75b9eb4f747f59bceba80d59 \
make ui
```

If `DOPPLER_ASSET_ADDRESS` is omitted, the app prompts for one at startup.

## UI

Screens:

- `1` Summary
- `2` Detail
- `3` Leaderboard
- `4` Comparison

Keys:

- `enter` load typed asset
- `a` enter another asset
- `j` / `k` move
- `h` / `l` move between markets in detail view
- `r` reload
- `q` quit

The detail and leaderboard tables hide zero-P&L wallets to reduce noise.

## Why a Market Might Fail

Usually one of these:

1. The asset does not resolve to a canonical pool.
2. The pool type appears aggregate-only in the public API.
3. The public API exposes too little raw swap data for wallet-level analysis.
4. The hosted API times out or returns an internal error.

This is why the app is intentionally conservative.

## Notes

- [INDEXER_DATA_NOTES.md](/Users/mohammadsyed/Source/doppler-sniper/INDEXER_DATA_NOTES.md)
- [INDEXER_ISSUE_DRAFT.md](/Users/mohammadsyed/Source/doppler-sniper/INDEXER_ISSUE_DRAFT.md)
