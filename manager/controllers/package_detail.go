package controllers

import (
	"app/base/database"
	"app/base/utils"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"net/http"
)

var PackageDetailSelect = database.MustGetSelect(&PackageDetailAttributes{})

type PackageDetailAttributes struct {
	Name        string `json:"name" query:"pn.name"`
	EVRA        string `json:"evra" query:"pkg.evra"`
	Description string `json:"description" query:"(select s.value from strings s where s.id = pkg.description_hash)"`
	Summary     string `json:"summary" query:"(select s.value from strings s where s.id = pkg.summary_hash)"`
	AdvisoryID  string `json:"advisory_id" query:"am.name"`
}

type PackageDetailItem struct {
	Attributes PackageDetailAttributes `json:"attributes"`
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
}

type PackageDetailResponse struct {
	Data PackageDetailItem `json:"data"`
}

func basePackageQuery(pkgName string) *gorm.DB {
	return database.Db.Table("package pkg").
		Select(PackageDetailSelect).
		Joins("JOIN package_name pn ON pkg.name_id = pn.id").
		Joins("JOIN advisory_metadata am on am.id = pkg.advisory_id").
		Where("pn.name = ?", pkgName)
}

func packageLatestHandler(c *gin.Context, packageName string) {
	query := basePackageQuery(packageName)
	var pkg PackageDetailAttributes
	err := query.Order("am.public_date DESC").Limit(1).Find(&pkg).Error

	if err != nil {
		LogAndRespNotFound(c, err, "package not found")
		return
	}

	nevra := packageName + "-" + pkg.EVRA

	c.JSON(200, PackageDetailResponse{
		Data: PackageDetailItem{
			Attributes: pkg,
			ID:         nevra,
			Type:       "package",
		},
	})
}

func packageEvraHandler(c *gin.Context, nevra *utils.Nevra) {
	query := basePackageQuery(nevra.Name)
	var pkg PackageDetailAttributes
	err := query.Where("pkg.evra = ?", nevra.EVRAString()).Find(&pkg).Error
	if err != nil {
		LogAndRespNotFound(c, err, "package not found")
		return
	}

	c.JSON(200, PackageDetailResponse{
		Data: PackageDetailItem{
			Attributes: pkg,
			ID:         nevra.String(),
			Type:       "package",
		},
	})
}

// @Summary Show me metadata of selected package
// @Description Show me metadata of selected package
// @ID LatestPackage
// @Security RhIdentity
// @Accept   json
// @Produce  json
// @Param    package_name    path    string   true "package_name - latest, nevra - exact version"
// @Success 200 {object} PackageDetailResponse
// @Router /api/patch/v1/packages/{package_name} [get]
func PackageDetailHandler(c *gin.Context) {
	parameter := c.Param("package_name")
	if parameter == "" {
		c.JSON(http.StatusBadRequest, utils.ErrorResponse{Error: "package_param not found"})
		return
	}

	nevra, err := utils.ParseNevra(parameter)
	if err == nil {
		packageEvraHandler(c, nevra)
	} else {
		packageLatestHandler(c, parameter)
	}
}
