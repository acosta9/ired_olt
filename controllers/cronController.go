package controllers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"ired.com/olt/app"
	"ired.com/olt/middlewares"
	"ired.com/olt/models"
	"ired.com/olt/repo"
)

func CronRoutes(r *gin.Engine) {
	cron := r.Group("/cron")
	{
		cron.GET("/olt-getclock", middlewares.BasicAuth(), getClock)
		cron.GET("/olt-getinfo", middlewares.BasicAuth(), oltInfo)
		cron.GET("/olt-autowrite", middlewares.BasicAuth(), oltAutoWrite)
		cron.GET("/olt-cleaning", middlewares.BasicAuth(), oltCleaning)
		cron.GET("/onu-getinfo", middlewares.BasicAuth(), onuInfo)
		cron.GET("/onu-traffic", middlewares.BasicAuth(), onuTraffic)
		cron.GET("/onu-cleaning", middlewares.BasicAuth(), onuCleaning)
	}
}

// @Summary 			Run the task get_clock
// @Description 	run cron to get status of clock on all olts
// @Tags 					Crons
// @Accept 				json
// @Produce 			json
// @Security 			BasicAuth
// @Success 			200 {object} models.SuccessResponse
// @Failure 			400 {object} models.ErrorResponse
// @Router 				/cron/olt-getclock [get]
func getClock(c *gin.Context) {
	//set variables for handling pgsql conn
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := models.ConnDb{Conn: app.PoolPgsql, Ctx: ctx}

	if err := repo.GetClock(db, "restApi"); err != nil {
		c.AbortWithStatusJSON(
			http.StatusBadRequest,
			models.ErrorResponse{Error: err.Error()},
		)
		return
	}

	c.JSON(
		http.StatusOK,
		models.SuccessResponse{Notice: "Cron Executed ok"},
	)
}

// @Summary 			Run the task get_olt_info
// @Description 	run cron to get olts general info
// @Tags 					Crons
// @Accept 				json
// @Produce 			json
// @Security 			BasicAuth
// @Success 			200 {object} models.SuccessResponse
// @Failure 			400 {object} models.ErrorResponse
// @Router 				/cron/olt-getinfo [get]
func oltInfo(c *gin.Context) {
	//set variables for handling pgsql conn
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := models.ConnDb{Conn: app.PoolPgsql, Ctx: ctx}

	if err := repo.OltInfo(db, "restApi"); err != nil {
		c.AbortWithStatusJSON(
			http.StatusBadRequest,
			models.ErrorResponse{Error: err.Error()},
		)
		return
	}

	c.JSON(
		http.StatusOK,
		models.SuccessResponse{Notice: "Cron Executed ok"},
	)
}

// @Summary 			Run the task olt_autowrite
// @Description 	run cron to write config to olt permanently
// @Tags 					Crons
// @Accept 				json
// @Produce 			json
// @Security 			BasicAuth
// @Success 			200 {object} models.SuccessResponse
// @Failure 			400 {object} models.ErrorResponse
// @Router 				/cron/olt-autowrite [get]
func oltAutoWrite(c *gin.Context) {
	//set variables for handling pgsql conn
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := models.ConnMysqlPgsql{ConnMysql: app.PoolMysql, ConnPgsql: app.PoolPgsql, Ctx: ctx}

	if err := repo.OltAutoWrite(db, "restApi"); err != nil {
		c.AbortWithStatusJSON(
			http.StatusBadRequest,
			models.ErrorResponse{Error: err.Error()},
		)
		return
	}

	c.JSON(
		http.StatusOK,
		models.SuccessResponse{Notice: "Cron Executed ok"},
	)
}

// @Summary 			Run the task clean_olt_data
// @Description 	run cron to clean old data relating olt in DB
// @Tags 					Crons
// @Accept 				json
// @Produce 			json
// @Security 			BasicAuth
// @Success 			200 {object} models.SuccessResponse
// @Failure 			400 {object} models.ErrorResponse
// @Router 				/cron/olt-cleaning [get]
func oltCleaning(c *gin.Context) {
	//set variables for handling pgsql conn
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := models.ConnDb{Conn: app.PoolPgsql, Ctx: ctx}

	if err := repo.CleanOltData(db, "restApi"); err != nil {
		c.AbortWithStatusJSON(
			http.StatusBadRequest,
			models.ErrorResponse{Error: err.Error()},
		)
		return
	}

	c.JSON(
		http.StatusOK,
		models.SuccessResponse{Notice: "Cron Executed ok"},
	)
}

// @Summary 			Run the task get_onu_info
// @Description 	run cron to get onus general info
// @Description 	-onu-status 	every 1min
// @Description 	-onu-rx 			every 5min
// @Description 	-onu-sn 			every 30min
// @Description 	-onu-name 		every 30min
// @Description 	-onu-ethlist	sinc manual
// @Description 	-onu-tx 			sinc manual 30min
// @Tags 					Crons
// @Accept 				json
// @Produce 			json
// @Security 			BasicAuth
// @Success 			200 {object} models.SuccessResponse
// @Failure 			400 {object} models.ErrorResponse
// @Router 				/cron/onu-getinfo [get]
func onuInfo(c *gin.Context) {
	//set variables for handling pgsql conn
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := models.ConnDb{Conn: app.PoolPgsql, Ctx: ctx}

	if err := repo.CronOnuInfo(db, "restApi"); err != nil {
		c.AbortWithStatusJSON(
			http.StatusBadRequest,
			models.ErrorResponse{Error: err.Error()},
		)
		return
	}

	c.JSON(
		http.StatusOK,
		models.SuccessResponse{Notice: "Cron Executed ok"},
	)
}

// @Summary 			Run the task get_onu_traffic
// @Description 	run cron to get onus traffic info
// @Tags 					Crons
// @Accept 				json
// @Produce 			json
// @Security 			BasicAuth
// @Success 			200 {object} models.SuccessResponse
// @Failure 			400 {object} models.ErrorResponse
// @Router 				/cron/onu-traffic [get]
func onuTraffic(c *gin.Context) {
	//set variables for handling pgsql conn
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := models.ConnDb{Conn: app.PoolPgsql, Ctx: ctx}

	if err := repo.CronOnuTraffic(db, "restApi"); err != nil {
		c.AbortWithStatusJSON(
			http.StatusBadRequest,
			models.ErrorResponse{Error: err.Error()},
		)
		return
	}

	c.JSON(
		http.StatusOK,
		models.SuccessResponse{Notice: "Cron Executed ok"},
	)
}

// @Summary 			Run the task clean_onu_data
// @Description 	run cron to clean old data relating to onus in DB
// @Tags 					Crons
// @Accept 				json
// @Produce 			json
// @Security 			BasicAuth
// @Success 			200 {object} models.SuccessResponse
// @Failure 			400 {object} models.ErrorResponse
// @Router 				/cron/onu-cleaning [get]
func onuCleaning(c *gin.Context) {
	//set variables for handling pgsql conn
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := models.ConnDb{Conn: app.PoolPgsql, Ctx: ctx}

	if err := repo.CleanOnuData(db, "restApi"); err != nil {
		c.AbortWithStatusJSON(
			http.StatusBadRequest,
			models.ErrorResponse{Error: err.Error()},
		)
		return
	}

	c.JSON(
		http.StatusOK,
		models.SuccessResponse{Notice: "Cron Executed ok"},
	)
}
