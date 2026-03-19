package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresDatabasePinger struct {
	Pool *pgxpool.Pool
}

func (p *PostgresDatabasePinger) CheckStatus(ctx context.Context) error {
	return p.Pool.Ping(ctx)
}
