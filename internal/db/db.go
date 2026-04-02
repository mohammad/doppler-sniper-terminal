package db

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	pool *pgxpool.Pool
	once sync.Once
)

func Init(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	var initErr error

	once.Do(func() {
		cfg, err := pgxpool.ParseConfig(databaseURL)
		if err != nil {
			initErr = fmt.Errorf("parse database config: %w", err)
			return
		}

		cfg.ConnConfig.RuntimeParams["application_name"] = "doppler-sniper"
		if schema := os.Getenv("DATABASE_SCHEMA"); schema != "" {
			cfg.ConnConfig.RuntimeParams["search_path"] = schema
		}
		cfg.ConnConfig.DefaultQueryExecMode = 5

		p, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			initErr = fmt.Errorf("create pool: %w", err)
			return
		}

		if err := p.Ping(ctx); err != nil {
			p.Close()
			initErr = fmt.Errorf("ping database: %w", err)
			return
		}

		pool = p
	})

	if initErr != nil {
		return nil, initErr
	}

	if pool == nil {
		return nil, fmt.Errorf("database pool not initialized")
	}

	return pool, nil
}

func GetPool() *pgxpool.Pool {
	return pool
}
