package controllers

import (
	"app/base/database"
	"app/manager/middlewares"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
)

var PackagesFields = database.MustGetQueryAttrs(&PackageItemQuery{})
var PackagesSelect = database.MustGetSelect(&PackageItemQuery{})
var PackagesOpts = ListOpts{
	Fields: PackagesFields,
	// By default, we show only fresh systems. If all systems are required, you must pass in:true,false filter into the api
	DefaultFilters: map[string]FilterData{},
	DefaultSort:    "name",
}

// Structure containing attributes shared between inner loading function and outer response
type PackageItemAttrs struct {
	Name             string `json:"name" query:"pn.name"`
	SystemsInstalled int    `json:"systems_installed" query:"count(sp.id)" aggregate:"true"`
	SystemsUpdatable int    `json:"systems_updatable" query:"count(sp.id) filter (where spkg.update_data is not null)" aggregate:"true"`
}

// Loading package data along with package_name
type PackageItemQuery struct {
	NameID int `query:"p.name_id"`
	PackageItemAttrs
}

type PackageItem struct {
	PackageItemAttrs
	Summary string `json:"summary" query:"sum.value"`
}

type PackagesResponse struct {
	Data  []PackageItem `json:"data"`
	Links Links         `json:"links"`
	Meta  ListMeta      `json:"meta"`
}

func packagesQuery(acc int) *gorm.DB {
	return database.Db.Debug().
		Select(PackagesSelect).
		Table("system_platform sp").
		Joins("inner join system_package spkg on spkg.system_id = sp.id and sp.stale = false").
		Joins("inner join package p on p.id = spkg.package_id").
		Joins("inner join package_name pn on pn.id = p.name_id").
		Where("spkg.rh_account_id = ?", acc).
		Group("p.name_id, pn.name")
}


// Loading package data along with package_name
type packageSummary struct {
	NameID  int
	Summary string
}

// Selects distinct values for each p.name id,
// order by clause ensures we get the latest released version
func packageDescQuery(names []int) *gorm.DB {
	return database.Db.Debug().
		Select("distinct on(p.name_id) p.name_id, sum.value").
		Table("package p").
		Joins("inner join strings sum on p.summary_hash = sum.id").
		Joins("inner join advisory_metadata am on p.advisory_id = am.id").
		Where("p.name_id in (?)", names).
		Order("p.name_id, am.public_date")
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

	query := packagesQuery(account)
	query, meta, links, err := ListCommon(query, c, "/packages", PackagesOpts)
	query = ApplySearch(c, query, "pn.name")
	query, _ = ApplyTagsFilter(c, query, "sp.inventory_id")

	if err != nil {
		LogAndRespError(c, err, "database error")
		return
	}
	// Loading just information based on package names
	var itemQ []PackageItemQuery
	err = query.Find(&itemQ).Error
	if err != nil {
		LogAndRespError(c, err, "database error")
		return
	}

	// We pick out the nameIds of returned packages
	nameIds := make([]int, len(itemQ))
	for i, s := range itemQ {
		nameIds[i] = s.NameID
	}

	// Find summaries of latest versions for given packages
	var summaries []packageSummary
	summaryQuery := packageDescQuery(nameIds)
	summaryQuery = ApplySearch(c, summaryQuery, "sum.value")
	if err := summaryQuery.Find(&summaries).Error; err != nil {
		LogAndRespError(c, err, "database error")
		return
	}

	// And assemble the results
	systems := make([]PackageItem,0, len(itemQ))
	for _, s := range itemQ {
		item := PackageItem{PackageItemAttrs: s.PackageItemAttrs}
		for _, sum := range summaries {
			if sum.NameID == s.NameID {
				item.Summary = sum.Summary
				break
			}
		}
		systems = append(systems, item)
	}

	c.JSON(200, PackagesResponse{
		Data:  systems,
		Links: *links,
		Meta:  *meta,
	})
}
