package app

import (
	"context"

	"github.com/jackc/pgx/pgxpool"
	// "github.com/jackc/pgx/log/logrusadapter"
)

type PgxConnPool struct {
	pool *pgxpool.Pool
	eng  *Engine
}

func InitPgxDriver(ctx context.Context, dburl string) (*PgxConnPool, error) {
	var err error
	pl := &PgxConnPool{}
	pl.pool, err = pgxpool.Connect(ctx, dburl)
	if err != nil {
		return nil, err
	}
	return pl, nil
}
