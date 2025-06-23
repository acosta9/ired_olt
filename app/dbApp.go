package app

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgxpool"
	"ired.com/olt/utils"
)

var PoolMysql *sql.DB
var PoolPgsql *pgxpool.Pool

func InitDbPgsql() {
	var err error
	var dbConfig *pgxpool.Config

	defaultMaxConns, _ := strconv.Atoi(os.Getenv("PGSQL_MAX_CONN"))
	defaultMinConns, _ := strconv.Atoi(os.Getenv("PGSQL_MIN_CONN"))

	const defaultMaxConnLifetime = time.Hour
	const defaultMaxConnIdleTime = time.Minute * 10
	const defaultHealthCheckPeriod = time.Minute
	const defaultConnectTimeout = time.Second * 10

	// Set up pool configuration
	dbConfig, err = pgxpool.ParseConfig(os.Getenv("DB_POSTGRES"))
	if err != nil {
		utils.Fatalf("Error reading variable to connect to postgresqlDB: %v", err)
	}

	dbConfig.MaxConns = int32(defaultMaxConns)
	dbConfig.MinConns = int32(defaultMinConns)
	dbConfig.MaxConnLifetime = defaultMaxConnLifetime
	dbConfig.MaxConnIdleTime = defaultMaxConnIdleTime
	dbConfig.HealthCheckPeriod = defaultHealthCheckPeriod
	dbConfig.ConnConfig.ConnectTimeout = defaultConnectTimeout

	PoolPgsql, err = pgxpool.NewWithConfig(context.Background(), dbConfig)
	if err != nil {
		utils.Fatalf("Error connecting to postgresqlDB: %v", err)
	}
}

func CloseDbPgsql() {
	PoolPgsql.Close()
}

func InitDbMysql() {
	var err error

	PoolMysql, err = sql.Open("mysql", os.Getenv("DB_MYSQL"))

	if err != nil {
		utils.Fatalf("Error connecting to mysql(iptv): %v", err)
	}

	// See "Important settings" section.
	defaultMaxConns, _ := strconv.Atoi(os.Getenv("MYSQL_MAX_CONN"))
	defaultMinConns, _ := strconv.Atoi(os.Getenv("MYSQL_MIN_CONN"))

	PoolMysql.SetMaxOpenConns(defaultMaxConns)
	PoolMysql.SetMaxIdleConns(defaultMinConns)
	PoolMysql.SetConnMaxLifetime(time.Hour)
	PoolMysql.SetConnMaxIdleTime(time.Minute * 15)
}

func CloseDbMysql() {
	PoolMysql.Close()
}
