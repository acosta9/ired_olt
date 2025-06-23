package middlewares

import (
	"encoding/base64"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

func BasicAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.Header("WWW-Authenticate", `Basic realm="Restricted"`)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		token := strings.Split(authHeader, "Basic ")
		if len(token) != 2 {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		decoded, err := base64.StdEncoding.DecodeString(token[1])
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		credentials := strings.Split(string(decoded), ":")
		if len(credentials) != 2 || credentials[0] != os.Getenv("DOC_USER") || credentials[1] != os.Getenv("DOC_PASSWD") {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		c.Next()
	}
}
