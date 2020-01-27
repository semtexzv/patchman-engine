package controllers

import (
	"app/base/core"
	"app/base/utils"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
	"net/http"
	"strings"
)

func LogAndRespError(c *gin.Context, err error, respMsg string) {
	utils.Log("err", err.Error()).Error(respMsg)
	c.JSON(http.StatusInternalServerError, utils.ErrorResponse{Error: respMsg})
}

func LogAndRespBadRequest(c *gin.Context, err error, respMsg string) {
	utils.Log("err", err.Error()).Warn(respMsg)
	c.JSON(http.StatusBadRequest, utils.ErrorResponse{Error: respMsg})
}

func LogAndRespNotFound(c *gin.Context, err error, respMsg string) {
	utils.Log("err", err.Error()).Warn(respMsg)
	c.JSON(http.StatusNotFound, utils.ErrorResponse{Error: respMsg})
}

func ApplySort(c *gin.Context, tx *gorm.DB, allowedFields ...string) (*gorm.DB, error) {
	query := c.DefaultQuery("sort", "id")
	fields := strings.Split(query, ",")

	allowedFieldSet := map[string]bool{
		"id": true,
	}

	for _, f := range allowedFields {
		allowedFieldSet[f] = true
	}
	for _, enteredField := range fields {
		if strings.HasPrefix(enteredField, "-") && allowedFieldSet[enteredField[1:]] { //nolint:gocritic
			tx = tx.Order(fmt.Sprintf("%v DESC", enteredField[1:]))
		} else if allowedFieldSet[enteredField] {
			tx = tx.Order(fmt.Sprintf("%v ASC", enteredField))
		} else {
			// We have not found any matches in allowed fields, return an error
			return nil, errors.Errorf("Invalid sort field: %v", enteredField)
		}
	}
	return tx, nil
}

type ListMeta struct {
	DataFormat string  `json:"data_format"`
	Filter     *string `json:"filter"`
	Limit      int     `json:"limit"`
	Offset     int     `json:"offset"`
	Advisory   string  `json:"advisory"`
	Page       int     `json:"page"`
	PageSize   int     `json:"page_size"`
	Pages      int     `json:"pages"`
	Enabled    bool    `json:"enabled"`
	TotalItems int     `json:"total_items"`
}

func BuildListMeta(tx *gorm.DB, c *gin.Context, allowedSortFields ...string) (*gorm.DB, ListMeta, error) {
	limit, offset, err := utils.LoadLimitOffset(c, core.DefaultLimit)

	tx, err = ApplySort(c, tx, allowedSortFields...)
	meta := ListMeta{
		Limit:  limit,
		Offset: offset,
		Page:       offset / limit,
		PageSize:   limit,
		Pages:      total / limit,
	}

	return tx, meta, err
}
