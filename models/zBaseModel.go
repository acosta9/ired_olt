package models

import (
	"context"
	"database/sql"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ErrorResponse struct {
	Error interface{} `json:"error"`
}

type SuccessResponse struct {
	Notice string      `json:"notice,omitempty"`
	Record interface{} `json:"record,omitempty"`
}

type ConnDb struct {
	Conn *pgxpool.Pool
	Ctx  context.Context
}

type WorkerStatus struct {
	TaskName    string         `json:"task"`
	PgsqlStatus PoolStatsPgsql `json:"PgsqlConns"`
}

type PoolStatsPgsql struct {
	AcquiredConns   int32  `json:"acquired"`
	TotalConns      int32  `json:"total"`
	IdleConns       int32  `json:"idle"`
	AcquireCount    int64  `json:"acquire_count"`
	AcquireDuration string `json:"acquire_duration"`
	// MaxConns             int32  `json:"max_conns"`
	// CanceledAcquireCount int64  `json:"canceled_acquire_count"`
	// ConstructingConns    int32  `json:"constructing_conns"`
	// EmptyAcquireCount    int64  `json:"empty_acquire_count"`
}

type WorkerStatusMysql struct {
	TaskName    string         `json:"task"`
	PgsqlStatus PoolStatsPgsql `json:"PgsqlConns"`
	MysqlStatus PoolStatsMysql `json:"MysqlConns"`
}

type ConnMysqlPgsql struct {
	ConnMysql *sql.DB
	ConnPgsql *pgxpool.Pool
	Ctx       context.Context
}

type PoolStatsMysql struct {
	MaxOpenConnections int    `json:"max_open"`
	OpenConnections    int    `json:"open"`
	InUse              int    `json:"in_use"`
	Idle               int    `json:"idle"`
	WaitCount          int64  `json:"wait_count"`
	WaitDuration       string `json:"wait_duration"`
	MaxIdleClosed      int64  `json:"max_idle_closed"`
	MaxLifetimeClosed  int64  `json:"max_lifetime_closed"`
}
