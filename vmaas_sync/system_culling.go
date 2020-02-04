package vmaas_sync //nolint:golint,stylecheck

import (
	"app/base/database"
	"app/base/utils"
	"time"
)

func deleteCulledSystems() {
	var culled int
	rows, err := database.Db.Exec("select culled from delete_culled_systems()").Rows()
	if err != nil {
		utils.Log("err", err.Error()).Error("Could not delete culled systems")
	}
	if rows.Next() {
		err = rows.Scan(&culled)
		if err != nil {
			utils.Log("err", err.Error()).Error("Could not load culled system count")
		}
		utils.Log("count", culled).Info("Deleted cylled systems")
	}
}
func RunSystemCulling() {
	ticker := time.NewTicker(time.Hour)

	for {
		<-ticker.C
		deleteCulledSystems()
	}
}
