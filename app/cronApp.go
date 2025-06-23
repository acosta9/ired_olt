package app

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/go-co-op/gocron/v2"
	"ired.com/olt/models"
	"ired.com/olt/repo"
	"ired.com/olt/utils"
)

// TaskConfig structure to hold the cron schedule and task name
type taskConfig struct {
	Schedule string `json:"schedule"`
	Task     string `json:"task"`
	Enabled  bool   `json:"enabled"`
}

// Load task configurations from file
func loadTasksConfig() ([]taskConfig, error) {
	// open file
	file, err := os.Open(".crontab")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// decode json data to struct
	var tasksConfig []taskConfig
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&tasksConfig)
	if err != nil {
		return nil, err
	}

	return tasksConfig, nil
}

func LoadCrontab() {

	// Load task configurations
	tasksConfig, err := loadTasksConfig()
	if err != nil {
		utils.Logline("Failed to load task configurations: %v", err)
		return
	}

	// Use America/Caracas time
	ccsLocation, _ := time.LoadLocation("America/Caracas")

	// Create a new scheduler
	scheduler, _ := gocron.NewScheduler(gocron.WithLocation(ccsLocation))

	// // Schedule tasks based on the configurations
	for _, taskConfig := range tasksConfig {
		if !taskConfig.Enabled {
			continue
		}
		var err error
		switch taskConfig.Task {
		case "get_clock":
			_, err = scheduler.NewJob(
				gocron.CronJob(taskConfig.Schedule, false),
				gocron.NewTask(getClock),
				gocron.WithSingletonMode(gocron.LimitModeReschedule),
			)
		case "clean_olt_data":
			_, err = scheduler.NewJob(
				gocron.CronJob(taskConfig.Schedule, false),
				gocron.NewTask(cleanOltData),
				gocron.WithSingletonMode(gocron.LimitModeReschedule),
			)
		case "get_olt_info":
			_, err = scheduler.NewJob(
				gocron.CronJob(taskConfig.Schedule, false),
				gocron.NewTask(getOltInfo),
				gocron.WithSingletonMode(gocron.LimitModeReschedule),
			)
		case "olt_autowrite":
			_, err = scheduler.NewJob(
				gocron.CronJob(taskConfig.Schedule, false),
				gocron.NewTask(oltAutoWrite),
				gocron.WithSingletonMode(gocron.LimitModeReschedule),
			)
		case "get_onu_info":
			_, err = scheduler.NewJob(
				gocron.CronJob(taskConfig.Schedule, false),
				gocron.NewTask(getOnuInfo),
				gocron.WithSingletonMode(gocron.LimitModeReschedule),
			)
		case "get_onu_traffic":
			_, err = scheduler.NewJob(
				gocron.CronJob(taskConfig.Schedule, false),
				gocron.NewTask(getOnuTraffic),
				gocron.WithSingletonMode(gocron.LimitModeReschedule),
			)
		case "clean_onu_data":
			_, err = scheduler.NewJob(
				gocron.CronJob(taskConfig.Schedule, false),
				gocron.NewTask(cleanOnuData),
				gocron.WithSingletonMode(gocron.LimitModeReschedule),
			)
		default:
			utils.Logline("Unknown task", taskConfig.Task)
		}

		if err != nil {
			utils.Logline("Failed to schedule task", err)
		}
	}

	// Start the scheduler
	scheduler.Start()
}

// Define  task functions
func getClock() {
	defer func() {
		if r := recover(); r != nil {
			utils.Logline("Recovered from panic <<get_clock>>: %v", r)
		}
	}()

	//set variables for handling pgsql conn
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db := models.ConnDb{Conn: PoolPgsql, Ctx: ctx}

	// run actual task
	if err := repo.GetClock(db, "cronJob"); err != nil {
		utils.Logline("Error on get_clock")
	}
}

func cleanOltData() {
	defer func() {
		if r := recover(); r != nil {
			utils.Logline("Recovered from panic <<clean_olt_data>>: %v", r)
		}
	}()

	//set variables for handling pgsql conn
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db := models.ConnDb{Conn: PoolPgsql, Ctx: ctx}

	// run actual task
	if err := repo.CleanOltData(db, "cronJob"); err != nil {
		utils.Logline("Error on clean_olt_data")
	}
}

func getOltInfo() {
	defer func() {
		if r := recover(); r != nil {
			utils.Logline("Recovered from panic <<get_olt_info>>: %v", r)
		}
	}()

	time.Sleep(30 * time.Second)

	//set variables for handling pgsql conn
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := models.ConnDb{Conn: PoolPgsql, Ctx: ctx}

	// run actual task
	if err := repo.OltInfo(db, "cronJob"); err != nil {
		utils.Logline("Error on get_olt_info")
	}
}

func oltAutoWrite() {
	defer func() {
		if r := recover(); r != nil {
			utils.Logline("Recovered from panic <<olt_autowrite>>: %v", r)
		}
	}()

	//set variables for handling pgsql conn
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()
	db := models.ConnMysqlPgsql{ConnPgsql: PoolPgsql, ConnMysql: PoolMysql, Ctx: ctx}

	// run actual task
	if err := repo.OltAutoWrite(db, "cronJob"); err != nil {
		utils.Logline("Error on olt_autowrite")
	}
}

func getOnuInfo() {
	defer func() {
		if r := recover(); r != nil {
			utils.Logline("Recovered from panic <<get_onu_info>>: %v", r)
		}
	}()

	//set variables for handling pgsql conn
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	db := models.ConnDb{Conn: PoolPgsql, Ctx: ctx}

	// run actual task
	if err := repo.CronOnuInfo(db, "cronJob"); err != nil {
		utils.Logline("Error on get_onu_info")
	}
}

func getOnuTraffic() {
	defer func() {
		if r := recover(); r != nil {
			utils.Logline("Recovered from panic <<get_onu_traffic>>: %v", r)
		}
	}()

	//set variables for handling pgsql conn
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()
	db := models.ConnDb{Conn: PoolPgsql, Ctx: ctx}

	// run actual task
	if err := repo.CronOnuTraffic(db, "cronJob"); err != nil {
		utils.Logline("Error on get_onu_traffic")
	}
}

func cleanOnuData() {
	defer func() {
		if r := recover(); r != nil {
			utils.Logline("Recovered from panic <<clean_onu_data>>: %v", r)
		}
	}()

	//set variables for handling pgsql conn
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db := models.ConnDb{Conn: PoolPgsql, Ctx: ctx}

	// run actual task
	if err := repo.CleanOnuData(db, "cronJob"); err != nil {
		utils.Logline("Error on clean_onu_data")
	}
}
