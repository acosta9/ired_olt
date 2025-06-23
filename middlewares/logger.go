package middlewares

import (
	"bytes"
	"io"
	"log"
	"time"

	"github.com/gin-gonic/gin"
)

func StartTimer() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Store the current time in the context
		c.Set("startTime", time.Now())
		c.Next()
	}
}

func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime, exists := c.Get("startTime")
		if !exists {
			startTime = time.Now() // Fallback to now if not set
		}

		// Log request URL first
		log.Printf("[GIN] %s | %3d | %13v | %15s | %-7s %s \n",
			time.Now().Format("2006/01/02 - 15:04:05"),
			c.Writer.Status(),
			time.Since(startTime.(time.Time)),
			c.ClientIP(),
			c.Request.Method,
			c.Request.RequestURI,
		)

		if gin.Mode() == gin.DebugMode {
			// Log request headers
			for k, v := range c.Request.Header {
				log.Printf("Header: %s=%s\n", k, v)
			}

			// Log request body
			var bodyBytes []byte
			if c.Request.Body != nil {
				bodyBytes, _ = io.ReadAll(c.Request.Body)
			}
			c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			log.Printf("Body: %s\n", bodyBytes)
		}

		c.Next()
	}
}
