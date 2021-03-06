package core

import (
	"app/base/database"
	"app/base/utils"
)

func ConfigureApp() {
	utils.ConfigureLogging()
	database.Configure()
}

func SetupTestEnvironment() {
	utils.SetenvOrFail("LOG_LEVEL", "debug")

	ConfigureApp()
	err := database.DelteAllHosts()
	if err != nil {
		panic(err)
	}
}
