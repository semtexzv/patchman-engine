package controllers

import (
	"app/base/database"
	"app/base/models"
	"app/base/utils"
	"app/manager/middlewares"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"net/http"
)

type SystemPackageResponse models.SystemPackageData

// @Summary Show me details about a system packages by given inventory id
// @Description Show me details about a system packages by given inventory id
// @ID systemPackages
// @Security RhIdentity
// @Accept   json
// @Produce  json
// @Param    inventory_id    path    string   true "Inventory ID"
// @Success 200 {object} models.SystemPackageData
// @Router /api/patch/v1/systems/{inventory_id}/packages [get]
func SystemPackagesHandler(c *gin.Context) {
	account := c.GetString(middlewares.KeyAccount)

	inventoryID := c.Param("inventory_id")
	if inventoryID == "" {
		c.JSON(http.StatusBadRequest, utils.ErrorResponse{Error: "inventory_id param not found"})
		return
	}

	var inventory models.SystemPlatform
	err := database.Db.
		Joins("inner join rh_account ra on system_platform.rh_account_id = ra.id").
		Where("ra.name = ?", account).
		Where("inventory_id = ?", inventoryID).
		Select("id, invetory_id, package_data").
		First(&inventory).Error
	if gorm.IsRecordNotFoundError(err) {
		LogAndRespNotFound(c, err, "inventory not found")
		return
	}

	if err != nil {
		LogAndRespError(c, err, "database error")
		return
	}

	c.Data(200, "application/json", inventory.PackageData.RawMessage)
}
