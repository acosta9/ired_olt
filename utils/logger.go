package utils

import (
	"encoding/json"
	"fmt"
	"log"
	"runtime"

	"ired.com/olt/models"
)

func Fatalf(format string, args ...interface{}) {
	// Get the file and line number
	_, file, line, _ := runtime.Caller(1)
	newFormat := fmt.Sprintf("%s:%d: %s", file, line, format)
	log.Fatalf(newFormat, args...)
}

func Logline(format string, args ...any) {
	// Get the file and line number
	_, file, line, _ := runtime.Caller(1)
	newFormat := fmt.Sprintf("%s:%d: %s", file, line, format)
	log.Println(newFormat, args)
}

// show status of db connections pgsql
func ShowStatusWorker(db models.ConnDb, taskName string, taskStatus string) string {
	// get stats from pgsql pool
	statsPgsql := db.Conn.Stat()

	// transform to json format
	poolStats := models.WorkerStatus{
		TaskName: taskName + "_" + taskStatus,
		PgsqlStatus: models.PoolStatsPgsql{
			AcquiredConns:   statsPgsql.AcquiredConns(),
			TotalConns:      statsPgsql.TotalConns(),
			IdleConns:       statsPgsql.IdleConns(),
			AcquireCount:    statsPgsql.AcquireCount(),
			AcquireDuration: statsPgsql.AcquireDuration().String(),
			// MaxConns:             statsPgsql.MaxConns(),
			// CanceledAcquireCount: statsPgsql.CanceledAcquireCount(),
			// ConstructingConns:    statsPgsql.ConstructingConns(),
			// EmptyAcquireCount:    statsPgsql.EmptyAcquireCount(),
		},
	}
	jsonData, err := json.Marshal(poolStats)
	if err != nil {
		Logline("Error parsing the json poolStat" + err.Error())
	}

	// print to logfile
	return string(jsonData)
}

// show status of db connections pgsql and mysql
func ShowStatusWorkerMysql(db models.ConnMysqlPgsql, taskName string, taskStatus string) string {
	// get stats from pgsql pool
	statsPgsql := db.ConnPgsql.Stat()

	// get stats from mysql pool
	statsMysql := db.ConnMysql.Stats()

	// transform to json format
	poolStats := models.WorkerStatusMysql{
		TaskName: taskName + "_" + taskStatus,
		PgsqlStatus: models.PoolStatsPgsql{
			AcquiredConns:   statsPgsql.AcquiredConns(),
			TotalConns:      statsPgsql.TotalConns(),
			IdleConns:       statsPgsql.IdleConns(),
			AcquireCount:    statsPgsql.AcquireCount(),
			AcquireDuration: statsPgsql.AcquireDuration().String(),
			// MaxConns:             statsPgsql.MaxConns(),
			// CanceledAcquireCount: statsPgsql.CanceledAcquireCount(),
			// ConstructingConns:    statsPgsql.ConstructingConns(),
			// EmptyAcquireCount:    statsPgsql.EmptyAcquireCount(),
		},
		MysqlStatus: models.PoolStatsMysql{
			MaxOpenConnections: statsMysql.MaxOpenConnections,
			OpenConnections:    statsMysql.OpenConnections,
			InUse:              statsMysql.InUse,
			Idle:               statsMysql.Idle,
			WaitCount:          statsMysql.WaitCount,
			WaitDuration:       statsMysql.WaitDuration.String(),
			MaxIdleClosed:      statsMysql.MaxIdleClosed,
			MaxLifetimeClosed:  statsMysql.MaxLifetimeClosed,
		},
	}
	jsonData, err := json.Marshal(poolStats)
	if err != nil {
		Logline("Error parsing the json poolStat" + err.Error())
	}

	// print to logfile
	return string(jsonData)
}
