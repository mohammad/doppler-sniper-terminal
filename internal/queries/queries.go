package queries

import (
	"context"
	"fmt"
	"strings"
	"time"

	"doppler-sniper/internal/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) ListMigratedLaunches(ctx context.Context, filter models.LaunchFilter) ([]models.Launch, error) {
	var (
		args   []any
		parts  []string
		cursor = 1
	)

	query := `
SELECT
    p.address,
    p.chain_id,
    p.asset,
    p.type,
    p.created_at,
    COALESCE(p.migrated_at, 0),
    COALESCE(p.total_tokens_sold, 0),
    COALESCE(t.symbol, ''),
    COALESCE(t.name, ''),
    COALESCE(t.image, '')
FROM pool p
LEFT JOIN token t ON t.address = p.asset AND t.chain_id = p.chain_id
WHERE p.migrated = true`

	if len(filter.ChainIDs) > 0 {
		parts = append(parts, fmt.Sprintf("p.chain_id = ANY($%d)", cursor))
		args = append(args, filter.ChainIDs)
		cursor++
	}

	if filter.CurveType != "" {
		switch filter.CurveType {
		case "multicurve":
			parts = append(parts, fmt.Sprintf("p.type ILIKE $%d", cursor))
			args = append(args, "%multicurve%")
			cursor++
		case "standard-v4":
			parts = append(parts, fmt.Sprintf("(p.type = $%d OR p.type = $%d)", cursor, cursor+1))
			args = append(args, "v4", "v4-decay")
			cursor += 2
		case "v3":
			parts = append(parts, fmt.Sprintf("p.type = $%d", cursor))
			args = append(args, "v3")
			cursor++
		}
	}

	if filter.DateRange != "" && filter.DateRange != "all" {
		days := map[string]int{"7d": 7, "30d": 30, "90d": 90}[filter.DateRange]
		if days > 0 {
			parts = append(parts, fmt.Sprintf("to_timestamp(p.migrated_at) >= $%d", cursor))
			args = append(args, time.Now().AddDate(0, 0, -days))
			cursor++
		}
	}

	if len(parts) > 0 {
		query += " AND " + strings.Join(parts, " AND ")
	}

	query += "\nORDER BY p.migrated_at DESC\nLIMIT $" + fmt.Sprint(cursor)
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list launches: %w", err)
	}
	defer rows.Close()

	launches, err := pgx.CollectRows(rows, pgx.RowToStructByPos[models.Launch])
	if err != nil {
		return nil, fmt.Errorf("collect launches: %w", err)
	}

	return launches, nil
}

func (s *Store) GetLaunch(ctx context.Context, poolAddress string, chainID int) (models.Launch, error) {
	query := `
SELECT
    p.address,
    p.chain_id,
    p.asset,
    p.type,
    p.created_at,
    COALESCE(p.migrated_at, 0),
    COALESCE(p.total_tokens_sold, 0),
    COALESCE(t.symbol, ''),
    COALESCE(t.name, ''),
    COALESCE(t.image, '')
FROM pool p
LEFT JOIN token t ON t.address = p.asset AND t.chain_id = p.chain_id
WHERE p.address = $1 AND p.chain_id = $2
LIMIT 1`

	row := s.pool.QueryRow(ctx, query, strings.ToLower(poolAddress), chainID)
	var launch models.Launch
	if err := row.Scan(
		&launch.Address,
		&launch.ChainID,
		&launch.Asset,
		&launch.Type,
		&launch.CreatedAt,
		&launch.MigratedAt,
		&launch.TotalTokensSold,
		&launch.Symbol,
		&launch.Name,
		&launch.Image,
	); err != nil {
		return models.Launch{}, fmt.Errorf("get launch: %w", err)
	}

	return launch, nil
}

func (s *Store) GetLaunchByAsset(ctx context.Context, assetAddress string, chainID int) (models.Launch, error) {
	query := `
SELECT
    p.address,
    p.chain_id,
    p.asset,
    p.type,
    p.created_at,
    COALESCE(p.migrated_at, 0),
    COALESCE(p.total_tokens_sold, 0),
    COALESCE(t.symbol, ''),
    COALESCE(t.name, ''),
    COALESCE(t.image, '')
FROM pool p
LEFT JOIN token t ON t.address = p.asset AND t.chain_id = p.chain_id
WHERE p.asset = $1 AND p.chain_id = $2
ORDER BY p.created_at DESC
LIMIT 1`

	row := s.pool.QueryRow(ctx, query, strings.ToLower(assetAddress), chainID)
	var launch models.Launch
	if err := row.Scan(
		&launch.Address,
		&launch.ChainID,
		&launch.Asset,
		&launch.Type,
		&launch.CreatedAt,
		&launch.MigratedAt,
		&launch.TotalTokensSold,
		&launch.Symbol,
		&launch.Name,
		&launch.Image,
	); err != nil {
		return models.Launch{}, fmt.Errorf("get launch by asset: %w", err)
	}

	return launch, nil
}

func (s *Store) GetLaunchSwaps(ctx context.Context, poolAddress string, chainID int, migratedAt int64) ([]models.Swap, error) {
	query := `
SELECT
    s.tx_hash,
    s.pool,
    s.chain_id,
    s."user",
    s.type,
    s.amount_in,
    s.amount_out,
    s.swap_value_usd,
    s.timestamp
FROM swap s
WHERE s.pool = $1
  AND s.chain_id = $2
  AND ($3 = 0 OR s.timestamp <= $3)
ORDER BY s.timestamp ASC, s.tx_hash ASC`

	rows, err := s.pool.Query(ctx, query, strings.ToLower(poolAddress), chainID, migratedAt)
	if err != nil {
		return nil, fmt.Errorf("get launch swaps: %w", err)
	}
	defer rows.Close()

	swaps, err := pgx.CollectRows(rows, pgx.RowToStructByPos[models.Swap])
	if err != nil {
		return nil, fmt.Errorf("collect swaps: %w", err)
	}

	return swaps, nil
}

func (s *Store) StreamLaunchSwaps(ctx context.Context, poolAddress string, chainID int, migratedAt int64, pageSize int, consume func([]models.Swap) error) error {
	if pageSize <= 0 {
		pageSize = 500
	}

	query := `
SELECT
    s.tx_hash,
    s.pool,
    s.chain_id,
    s."user",
    s.type,
    s.amount_in,
    s.amount_out,
    s.swap_value_usd,
    s.timestamp
FROM swap s
WHERE s.pool = $1
  AND s.chain_id = $2
  AND ($3 = 0 OR s.timestamp <= $3)
ORDER BY s.timestamp ASC, s.tx_hash ASC`

	rows, err := s.pool.Query(ctx, query, strings.ToLower(poolAddress), chainID, migratedAt)
	if err != nil {
		return fmt.Errorf("stream launch swaps: %w", err)
	}
	defer rows.Close()

	page := make([]models.Swap, 0, pageSize)
	for rows.Next() {
		var swap models.Swap
		if err := rows.Scan(
			&swap.TxHash,
			&swap.Pool,
			&swap.ChainID,
			&swap.User,
			&swap.Type,
			&swap.AmountIn,
			&swap.AmountOut,
			&swap.SwapValueUSD,
			&swap.Timestamp,
		); err != nil {
			return fmt.Errorf("scan streamed swap: %w", err)
		}
		page = append(page, swap)
		if len(page) == pageSize {
			if err := consume(page); err != nil {
				return err
			}
			page = make([]models.Swap, 0, pageSize)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate streamed swaps: %w", err)
	}
	if len(page) > 0 {
		if err := consume(page); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CountLaunchSwaps(ctx context.Context, poolAddress string, chainID int, migratedAt int64) (int, error) {
	var count int
	query := `
SELECT COUNT(*)
FROM swap s
WHERE s.pool = $1
  AND s.chain_id = $2
  AND ($3 = 0 OR s.timestamp <= $3)`

	if err := s.pool.QueryRow(ctx, query, strings.ToLower(poolAddress), chainID, migratedAt).Scan(&count); err != nil {
		return 0, fmt.Errorf("count launch swaps: %w", err)
	}

	return count, nil
}

func (s *Store) GetOHLCBuckets(ctx context.Context, poolAddress string, chainID int) ([]models.OHLCBucket, error) {
	query := `
SELECT
    minute_id,
    open,
    close,
    high,
    low,
    volume_usd,
    count
FROM fifteen_minute_bucket_usd
WHERE pool = $1
  AND chain_id = $2
ORDER BY minute_id ASC`

	rows, err := s.pool.Query(ctx, query, strings.ToLower(poolAddress), chainID)
	if err != nil {
		return nil, fmt.Errorf("get buckets: %w", err)
	}
	defer rows.Close()

	buckets, err := pgx.CollectRows(rows, pgx.RowToStructByPos[models.OHLCBucket])
	if err != nil {
		return nil, fmt.Errorf("collect buckets: %w", err)
	}

	return buckets, nil
}

func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *Store) HasRequiredSchema(ctx context.Context) (bool, error) {
	var poolExists bool
	if err := s.pool.QueryRow(ctx, `SELECT to_regclass('pool') IS NOT NULL`).Scan(&poolExists); err != nil {
		return false, fmt.Errorf("check pool table: %w", err)
	}
	return poolExists, nil
}

func (s *Store) CountCheckpoints(ctx context.Context) (int, error) {
	var count int
	query := `
SELECT CASE
	WHEN to_regclass('_ponder_checkpoint') IS NULL THEN 0
	ELSE (SELECT COUNT(*) FROM _ponder_checkpoint)
END`
	if err := s.pool.QueryRow(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("count checkpoints: %w", err)
	}
	return count, nil
}

func (s *Store) IsIndexerReady(ctx context.Context) (bool, error) {
	var ready bool
	query := `
SELECT CASE
	WHEN to_regclass('_ponder_meta') IS NULL THEN false
	ELSE COALESCE((
		SELECT ((value->>'is_ready')::int = 1)
		FROM _ponder_meta
		WHERE key = 'app'
		LIMIT 1
	), false)
END`
	if err := s.pool.QueryRow(ctx, query).Scan(&ready); err != nil {
		return false, fmt.Errorf("check indexer ready state: %w", err)
	}
	return ready, nil
}

func (s *Store) GetLatestSwapTimestamp(ctx context.Context) (int64, error) {
	var latest int64
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(MAX(timestamp), 0) FROM swap`).Scan(&latest); err != nil {
		return 0, fmt.Errorf("get latest swap timestamp: %w", err)
	}
	return latest, nil
}
