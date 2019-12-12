package vmaas_sync

import (
	"app/base"
	"app/base/database"
	"app/base/models"
	"app/base/utils"
	"context"
	"github.com/RedHatInsights/patchman-clients/vmaas"
	"github.com/antihax/optional"
	"time"
)

var (
	vmaasClient *vmaas.APIClient
)

func configure() {
	cfg := vmaas.NewConfiguration()
	cfg.BasePath = utils.GetenvOrFail("VMAAS_ADDRESS") + base.VMAAS_API_PREFIX
	cfg.Debug = true

	vmaasClient = vmaas.NewAPIClient(cfg)
}

func storeErratas(data map[string]vmaas.ErrataResponseErrataList) error {
	var erratas models.ErrataMetaDataSlice

	for n, v := range data {
		// Should we skip or report invalid erratas ?
		issued, err := time.Parse(base.RFC_3339_NO_TZ, v.Issued)
		if err != nil {
			utils.Log("err", err.Error()).Warn("Invalid errata issued date")
			continue
		}
		modified, err := time.Parse(base.RFC_3339_NO_TZ, v.Updated)
		if err != nil {
			utils.Log("err", err.Error()).Warn("Invalid errata modified date")
			continue
		}
		if v.Description == "" || v.Summary == "" {
			continue
		}

		erratum := models.ErrataMetaData{
			Advisory:     n,
			AdvisoryName: n,
			Description:  v.Description,
			Synopsis:     v.Synopsis,
			Topic:        v.Type,
			Solution:     v.Solution,
			PublicDate:   issued,
			ModifiedDate: modified,
			Url:          &v.Url,
		}

		erratas = append(erratas, erratum)
	}

	if erratas == nil || len(erratas) == 0 {
		return nil
	}

	tx := database.OnConflictUpdate(database.Db, "advisory", "synopsis", "topic", "solution", "public_date", "modified_date", "url")
	errors := database.BulkInsertChunk(tx, erratas.ToInterfaceSlice(), 1000)
	if errors != nil && len(errors) > 0 {
		return errors[0]
	}
	return nil

}

func syncErratas() {
	ctx := context.Background()

	if vmaasClient == nil {
		panic("VMaaS client is nil")
	}

	pageIdx := 0
	maxPageIdx := 1

	for pageIdx <= maxPageIdx {

		opts := vmaas.AppErrataHandlerPostPostOpts{
			ErrataRequest: optional.NewInterface(vmaas.ErrataRequest{
				Page:          float32(pageIdx),
				PageSize:      1000,
				ErrataList:    []string{".*"},
				ModifiedSince: "",
			}),
		}

		data, _, err := vmaasClient.ErrataApi.AppErrataHandlerPostPost(ctx, &opts)

		if err != nil {
			panic(err)
		}

		maxPageIdx = int(data.Pages)
		pageIdx += 1

		utils.Log("count", len(data.ErrataList)).Debug("Downloaded erratas")

		err = storeErratas(data.ErrataList)
		if err != nil {
			panic(err)
		}
	}
}
