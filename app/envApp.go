package app

import (
	"os"

	"github.com/joho/godotenv"
	"ired.com/olt/utils"
)

func LoadEnvVariables() {
	err := godotenv.Load()

	if err != nil {
		utils.Fatalf("Error loading .env file %v", err)
		os.Exit(1)
	}
}
