package controllers

import (
	"app/base/database"
	"app/manager/middlewares"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
)

var PackagesFields = database.MustGetQueryAttrs(&PackageItem{})
var PackagesSelect = database.MustGetSelect(&PackageItem{})
var PackagesOpts = ListOpts{
	Fields: PackagesFields,
	// By default, we show only fresh systems. If all systems are required, you must pass in:true,false filter into the api
	DefaultFilters: map[string]FilterData{},
	DefaultSort:    "name",
}

type PackageItem struct {
	Name             string `json:"name" query:"detail.name"`
	SystemsInstalled int    `json:"systems_installed" query:"detail.systems_installed"`
	SystemsUpdatable int    `json:"systems_updatable" query:"detail.systems_updatable"`
	Summary          string `json:"summary" query:"sum.summary"`
}

type PackagesResponse struct {
	Data  []PackageItem `json:"data"`
	Links Links         `json:"links"`
	Meta  ListMeta      `json:"meta"`
}

var packagesLoadSelect = database.MustGetSelect(&packageLoadAttrs{})

type packageLoadAttrs struct {
	NameID           int    `query:"p.name_id"`
	Name             string `json:"name" query:"pn.name"`
	SystemsInstalled int    `json:"systems_installed" query:"count(sp.id)"`
	SystemsUpdatable int    `json:"systems_updatable" query:"count(sp.id) filter (where spkg.update_data is not null)"`
}

func packagesInnerQuery(acc int) *gorm.DB {
	return database.Db.Debug().
		Select(packagesLoadSelect).
		Table("system_platform sp").
		Joins("inner join system_package spkg on spkg.system_id = sp.id and sp.stale = false").
		Joins("inner join package p on p.id = spkg.package_id").
		Joins("inner join package_name pn on pn.id = p.name_id").
		Where("spkg.rh_account_id = ?", acc).
		Group("p.name_id, pn.name")
}

// Selects distinct values for each p.name id,
// order by clause ensures we get the latest released version
func packagesSumQuery(account int) *gorm.DB {
	return database.Db.Debug().
		Select("distinct on(p.name_id) p.name_id, am.summary").
		Table("package p").
		Joins("inner join strings s on p.summary_hash = s.id").
		Joins("join advisory_metadata am on p.advisory_id = am.id").
		Order("p.name_id, am.public_date")
}

func packagesFinalQuery(account int) *gorm.DB {
	packages := packagesInnerQuery(account).SubQuery()
	sums := packagesSumQuery(account).SubQuery()
	return database.Db.Debug().
		Table("package_name pn").
		Select(PackagesSelect).
		Joins("INNER JOIN ? detail on pn.id = detail.name_id", packages).
		Joins("INNER JOIN ? sum on pn.id = sum.name_id", sums)
}

// @Summary Show me all installed packages across my systems
// @Description Show me all installed packages across my systems
// @ID listPackages
// @Security RhIdentity
// @Accept   json
// @Produce  json
// @Param    limit          query   int     false   "Limit for paging, set -1 to return all"
// @Param    offset         query   int     false   "Offset for paging"
// @Param    sort           query   string  false   "Sort field" Enums(id,name,systems_installed,systems_updatable)
// @Param    search         query   string  false   "Find matching text"
// @Param    filter[name]    query   string  false "Filter"
// @Param    filter[systems_installed] query   string  false "Filter"
// @Param    filter[systems_updatable] query   string  false "Filter"
// @Param    tags                    query   []string  false "Tag filter"
// @Success 200 {object} PackagesResponse
// @Router /api/patch/v1/packages/ [get]
func PackagesListHandler(c *gin.Context) {
	account := c.GetInt(middlewares.KeyAccount)

	query := packagesFinalQuery(account)
	query, meta, links, err := ListCommon(query, c, "/packages", PackagesOpts)
	query = ApplySearch(c, query, "pn.name")
	query, _ = ApplyTagsFilter(c, query, "sp.inventory_id")

	if err != nil {
		LogAndRespError(c, err, "database error")
		return
	}
	// Loading just information based on package names
	var systems []PackageItem
	err = query.Find(&systems).Error
	if err != nil {
		LogAndRespError(c, err, "database error")
		return
	}

	c.JSON(200, PackagesResponse{
		Data:  systems,
		Links: *links,
		Meta:  *meta,
	})
}
