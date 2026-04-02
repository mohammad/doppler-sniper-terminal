# doppler-sniper

A terminal-first Go application for detecting and exploring sniper activity across Doppler token
launches. It reads from Doppler's hosted testnet GraphQL indexer and stays read-only.

---

## What It Does

`doppler-sniper` is an empirical companion to the paper **"Price Discovery Auctions"** by Austin
Adams (Whetstone Research, November 2025). The core question is whether Doppler's Multicurve-style
launches reduce sniper extraction relative to more standard launch curves.

The app classifies wallets per launch into:

- `SNIPER`: bought early and exited at least half their position before migration
- `BELIEVER`: bought early and mostly held through migration
- `MIXED`: bought early but partially exited
- `LATE_BUYER`: first buy happened after the early-entry threshold

From those classifications it computes:

- Sniper pressure score
- Total realized sniper extraction in USD
- Believer loss
- Wallet-level P&L
- Cross-launch leaderboard and curve-type comparison summaries

---

## Architecture

This is now a **terminal UI** rather than a web app.

Tech stack:

- Go 1.22+
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) for the TUI
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) for terminal styling
- Hosted Doppler GraphQL indexer
- `joho/godotenv` for `.env` loading

The app keeps the original backend split:

- `internal/app`: application orchestration for loading launch analysis, building summaries, and runtime inspection
- `internal/indexerapi`: hosted GraphQL indexer client
- `internal/detector`: sniper classification logic
- `internal/metrics`: pressure score, medians, believer loss
- `internal/models`: shared types
- `cmd/server/main.go`: Bubble Tea entrypoint, UI state, and screen rendering

---

## Terminal UI

The TUI has four screens:

1. Explorer
   Shows migrated launches with chain, curve, and date filters.

2. Deep Dive
   Shows per-launch summary metrics, top wallets, and recent swaps.

3. Leaderboard
   Ranks sniper wallets by cumulative realized profit.

4. Comparison
   Compares Multicurve, Standard V4, and V3 launches via median outcomes.

Keyboard controls:

- `1` explorer
- `2` detail
- `3` leaderboard
- `4` comparison
- `j` / `k` move
- `enter` open selected launch
- `h` / `l` move between launches in detail view
- `t` cycle curve filter
- `d` cycle date filter
- `r` reload launch data
- `q` quit

---

## Hosted Mode

The default OSS path is the hosted testnet indexer:

```env
INDEXER_GRAPHQL_URL=https://testnet-indexer.doppler.lol/graphql
DOPPLER_CHAIN_ID=84532
```

This endpoint currently exposes `baseSepolia` and `sepolia`, so `doppler-sniper` is now a Base
Sepolia development path rather than a Base mainnet analytics feed.

---

## Runtime Status Awareness

The terminal UI shows a lightweight status line near the top.

- GraphQL endpoint reachability
- Latest indexed swap timestamp

---

## Project Structure

```text
doppler-sniper/
├── cmd/
│   └── server/
│       └── main.go
├── internal/
│   ├── app/
│   │   ├── compare.go
│   │   ├── runtime.go
│   │   └── service.go
│   ├── detector/
│   │   └── detector.go
│   ├── indexerapi/
│   │   └── repository.go
│   ├── metrics/
│   │   └── metrics.go
│   ├── models/
│   │   └── models.go
├── .env.example
├── Makefile
├── go.mod
├── go.sum
└── README.md
```

---

## Environment Setup

### Prerequisites

- Go 1.22+
- `rg` (ripgrep)

### Config

Copy `.env.example` to `.env`:

```env
INDEXER_GRAPHQL_URL=https://testnet-indexer.doppler.lol/graphql
DOPPLER_CHAIN_ID=84532
PORT=8080
```

`PORT` is left in place for compatibility but is not used by the terminal interface.

Important:

- `doppler-sniper` reads its own `.env` file in this repo.
- No sibling repo, Docker, or local Postgres is required.

---

## Make Targets

From `doppler-sniper`:

```bash
# Run the terminal UI
make ui

# Build the binary
make build

# Tidy Go dependencies
make tidy
```

---

## Running

Typical flow:

```bash
# 1. Create the local env file
cp .env.example .env

# 2. Run the TUI
make ui
```

---

## Data Model

The hosted GraphQL endpoint exposes the same high-level entities the app uses for analysis:

- `pools`
- `swaps`
- `fifteenMinuteBucketUsds`
- `_meta`

Reference: [Doppler Indexer API](https://docs.doppler.lol/reference/api-usage)

---

## Sniper Detection Algorithm

A wallet is a sniper on a given launch if both hold:

1. First buy occurred before the pool sold 20% of total tokens.
2. The wallet sold at least 50% of its bonding-curve token position before or at migration.

Pseudocode:

```text
For each migrated pool:
  1. Fetch swaps ordered by timestamp ascending.
  2. Walk buy swaps and track cumulative tokens sold.
  3. The timestamp where cumulative sold first crosses 20% of total_tokens_sold is the early threshold.

  For each wallet:
    buys_pre_migration  = all buy swaps for that wallet before migration
    sells_pre_migration = all sell swaps for that wallet before migration

    first_buy_ts  = min(buy timestamps)
    is_early      = first_buy_ts <= early_threshold_ts
    tokens_bought = sum(amount_out for buys)
    tokens_sold   = sum(amount_in for sells)
    sell_ratio    = tokens_sold / tokens_bought

    if is_early and sell_ratio >= 0.50:
      classification = SNIPER
    elif is_early and sell_ratio < 0.20:
      classification = BELIEVER
    elif is_early:
      classification = MIXED
    else:
      classification = LATE_BUYER

    profit_usd = sum(sell.swap_value_usd) - sum(buy.swap_value_usd)
```

Sniper pressure score:

```text
pressure_score = sniper_buy_usd / total_pre_migration_usd * 100
```

Lower is healthier.

---

## Design Choices

- Supply-based early-entry threshold instead of a time threshold
- Raw SQL over an ORM
- Read-only access to the sibling indexer database
- Terminal UI over a browser UI for a tighter local analysis workflow
- Reuse of `swap_value_usd` instead of rebuilding quote-token conversion logic

---

## Current State

Implemented:

- Go module and dependency setup
- Read-only Postgres integration
- Typed raw-query layer
- Sniper detection and metric computation
- Explorer, deep-dive, leaderboard, and comparison terminal screens
- Local status awareness for database and sibling indexer process
- Make targets for common local workflows

Still worth improving:

- In-terminal sorting/searching
- Richer launch detail views
- Optional export modes for CSV / JSON
- Better process detection and freshness heuristics
