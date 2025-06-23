package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
	"ired.com/olt/app"
	"ired.com/olt/controllers"
	"ired.com/olt/middlewares"

	"github.com/gin-gonic/gin"
)

func init() {
	app.LoadEnvVariables()
	app.InitDbPgsql()
	app.InitDbMysql()
	app.LoadCrontab()

	gin.SetMode(os.Getenv("GIN_MODE"))

	// create log file and handle logrotate also
	logfile := &lumberjack.Logger{
		Filename:   "logs/main.log",
		MaxSize:    100,  // megabytes
		MaxBackups: 30,   // Keep logs for a month
		Compress:   true, // disabled by default
	}
	log.SetOutput(logfile)

	//manual function to rotate logs daily
	go func() {
		for range time.Tick(24 * time.Hour) {
			logfile.Rotate()
		}
	}()
}

// @Title								Olt Service API
// @Version							1.0
// @Description 				service in Go using Gin framework
// @Host								127.0.0.1:7002
// @Contact.name   			Juan Acosta
// @Contact.url			    https://www.linkedin.com/in/juan-m-acosta-f-54219758/
// @Contact.email  			juan9acosta@gmail.com
// @securityDefinitions.basic BasicAuth
// @securityDefinitions.basic.description Basic Authentication
// @BasePath /
func main() {
	r := gin.Default()

	// aply startTimer middleware
	r.Use(middlewares.StartTimer())

	// apply custom logger middleware
	r.Use(middlewares.RequestLogger())

	// recover if panic and log the fail
	r.Use(gin.RecoveryWithWriter(log.Writer()))

	// load templates
	r.LoadHTMLGlob("templates/*")

	// load static files
	r.Static("/public", "./public")

	// manual routes
	controllers.CronRoutes(r)

	// load docs
	controllers.SwaggerRoutes(r)

	// handle 404 error with custom template
	r.NoRoute(func(c *gin.Context) {
		c.HTML(http.StatusNotFound, "404.tmpl", gin.H{
			"title": "Page Not Found",
		})
	})

	// run server default port 8080 or lookup .env file
	r.Run()

	defer app.CloseDbPgsql()
	defer app.CloseDbMysql()
}
