package evaluator

import (
	"app/base"
	"app/base/database"
	"app/base/models"
	"app/base/utils"
	"context"
	"github.com/RedHatInsights/patchman-clients/vmaas"
	"github.com/antihax/optional"
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
	"time"
)

const unknown = "unknown"

var (
	vmaasClient *vmaas.APIClient
)

func Configure() {
	traceApi := utils.GetenvOrFail("LOG_LEVEL") == "trace"

	vmaasConfig := vmaas.NewConfiguration()
	vmaasConfig.BasePath = utils.GetenvOrFail("VMAAS_ADDRESS") + base.VMAAS_API_PREFIX
	vmaasConfig.Debug = traceApi
	vmaasClient = vmaas.NewAPIClient(vmaasConfig)
}

func Evaluate(systemID int, ctx context.Context, updatesReq vmaas.UpdatesRequest) {
	vmaasCallArgs := vmaas.AppUpdatesHandlerV2PostPostOpts{
		UpdatesRequest: optional.NewInterface(updatesReq),
	}

	vmaasData, _, err := vmaasClient.UpdatesApi.AppUpdatesHandlerV2PostPost(ctx, &vmaasCallArgs)
	if err != nil {
		utils.Log("err", err.Error()).Error("Unable to get updates from VMaaS")
		return
	}

	tx := database.Db.Begin()
	err = processSystemAdvisories(tx, systemID, vmaasData)
	if err != nil {
		tx.Rollback()
		utils.Log("err", err.Error()).Error("Unable to process system advisories")
		return
	}

	tx.Commit()
}

func processSystemAdvisories(tx *gorm.DB, systemID int, vmaasData vmaas.UpdatesV2Response) error {
	reported := getReportedAdvisories(vmaasData)
	stored, err := getSystemAdvisoriesMap(tx, systemID)

	if err != nil {
		return errors.Wrap(err, "Unable to get system stored advisories")
	}
	// Advisories which need to be stored
	var modified models.SystemAdvisoriesSlice
	// Names of newly unpatched advisories, newly patched advisories
	unpatchedNames, patched := walkAdvisories(reported, stored)

	// Set the when_patched field for every newly patched advisory
	now := time.Now()
	for _, p := range patched {
		p.WhenPatched = &now
		modified = append(modified, p)
	}

	// Retrieve metadata for advisories, which
	// TODO  load metadata by names
	unpatchedAdvisories, err := getStoredAdvisoriesMap(tx.Where("name in (?)", unpatchedNames).Preload("Advisory"))

	for n := range unpatchedNames {
		// Default case, if we have an advisory
		if adv, has := unpatchedAdvisories[n]; has {
			adv.WhenPatched = nil
			modified = append(modified, adv)
		} else {
			// Exeptional case, we do not have advisory metadata stored, can happen if evaluation happens in the middle
			// of vmaas-sync
			// Create new advisories one by one, and append to modifed list
			md := models.AdvisoryMetadata{
				Name:        n,
				Description: unknown,
				Synopsis:    unknown,
				Summary:     unknown,
				Solution:    unknown,
			}
			err = tx.Create(&md).Error
			if err != nil {
				panic(err)
			}
			modified = append(modified, models.SystemAdvisories{
				SystemID:   systemID,
				AdvisoryID: md.ID,
			})
		}
	}
	// Insert, and on conflict create change the when_patched field
	insertQ := database.OnConflictUpdate(tx, "id", "when_patched")
	errs := database.BulkInsertChunk(insertQ, modified.ToInterfaceSlice(), 1000)

	if errs != nil && len(errs) > 0 {
		return errors.Wrap(errs[0], "Unable to update system advisories")
	}
	return nil
}

func getReportedAdvisories(vmaasData vmaas.UpdatesV2Response) map[string]bool {
	advisories := map[string]bool{}
	for _, updates := range vmaasData.UpdateList {
		for _, update := range updates.AvailableUpdates {
			advisories[update.Erratum] = true
		}
	}
	return advisories
}

func getStoredAdvisoriesMap(tx *gorm.DB) (map[string]models.SystemAdvisories, error) {
	var advisories []models.SystemAdvisories
	err := tx.Preload("Advisory").Find(&advisories).Error
	if err != nil {
		return nil, err
	}

	advisoriesMap := map[string]models.SystemAdvisories{}
	for _, advisory := range advisories {
		advisoriesMap[advisory.Advisory.Name] = advisory
	}
	return advisoriesMap, nil
}

func getSystemAdvisoriesMap(tx *gorm.DB, systemID int) (map[string]models.SystemAdvisories, error) {
	return getStoredAdvisoriesMap(database.SystemAdvisoriesQueryByID(tx, systemID))
}

func walkAdvisories(reported map[string]bool, stored map[string]models.SystemAdvisories) (map[string]struct{}, map[string]models.SystemAdvisories) {

	unpatched := map[string]struct{}{}
	patched := map[string]models.SystemAdvisories{}

	for advisory := range reported {
		if sa, has := stored[advisory]; !has || sa.WhenPatched != nil {
			unpatched[advisory] = struct{}{}
		}
	}
	for advisory := range stored {
		if _, has := reported[advisory]; !has {
			patched[advisory] = stored[advisory]
		}
	}

	unpatchedArr := []string{}
	for a := range unpatched {
		unpatchedArr = append(unpatchedArr, a)
	}
	return unpatchedArr, patched
}

func getNewAndUnpatchedAdvisories(reported map[string]bool, stored map[string]models.SystemAdvisories) (
	[]string, []int) {
	newAdvisories := []string{}
	unpatchedAdvisories := []int{}
	for reportedAdvisory, _ := range reported {
		if storedAdvisory, found := stored[reportedAdvisory]; found {
			if storedAdvisory.WhenPatched != nil { // this advisory was already patched and now is un-patched again
				unpatchedAdvisories = append(unpatchedAdvisories, storedAdvisory.AdvisoryID)
			}
			utils.Log("advisory", storedAdvisory.Advisory.Name).Debug("still not patched")
		} else {
			newAdvisories = append(newAdvisories, reportedAdvisory)
		}
	}
	return newAdvisories, unpatchedAdvisories
}

func getPatchedAdvisories(reported map[string]bool, stored map[string]models.SystemAdvisories) []int {
	var patchedAdvisories []int
	for storedAdvisory, storedAdvisoryObj := range stored {
		if _, found := reported[storedAdvisory]; found {
			continue
		}

		// advisory contained in reported - it's patched
		if storedAdvisoryObj.WhenPatched != nil {
			// it's already marked as patched
			continue
		}

		// advisory was patched from last evaluation, let's mark it as patched
		patchedAdvisories = append(patchedAdvisories, storedAdvisoryObj.AdvisoryID)
	}
	return patchedAdvisories
}

func updateSystemAdvisoriesWhenPatched(tx *gorm.DB, systemID int, advisoryIDs []int, whenPatched *time.Time) error {
	err := tx.Model(models.SystemAdvisories{}).
		Where("system_id = ? AND advisory_id IN (?)", systemID, advisoryIDs).
		Update("when_patched", whenPatched).Error
	return err
}

// Return advisory IDs, created advisories count, error
func ensureAdvisoriesInDb(tx *gorm.DB, advisories []string) (*[]int, int, error) {
	var existingAdvisories []models.AdvisoryMetadata
	err := tx.Where("name IN (?)", advisories).Find(&existingAdvisories).Error
	if err != nil {
		return nil, 0, err
	}

	var existingAdvisoryIDs []int
	for _, existingAdvisory := range existingAdvisories {
		existingAdvisoryIDs = append(existingAdvisoryIDs, existingAdvisory.ID)
	}

	if len(existingAdvisories) == len(advisories) {
		// all advisories are in database
		return &existingAdvisoryIDs, 0, nil
	}

	createdAdvisoryIDs, err := createNewAdvisories(tx, advisories, existingAdvisories)
	if err != nil {
		return nil, 0, err
	}
	existingAdvisoryIDs = append(existingAdvisoryIDs, *createdAdvisoryIDs...)

	return &existingAdvisoryIDs, len(*createdAdvisoryIDs), nil
}

// Return created advisories IDs, created advisories, error
func createNewAdvisories(tx *gorm.DB, advisories []string, existingAdvisories []models.AdvisoryMetadata) (
	*[]int, error) {
	existingAdvisoriesMap := map[string]bool{}
	for _, advisoryObj := range existingAdvisories {
		existingAdvisoriesMap[advisoryObj.Name] = true
	}

	var createdAdvisoryIDs []int
	for _, advisory := range advisories {
		if _, found := existingAdvisoriesMap[advisory]; found {
			continue // advisory is already stored in database
		}

		item := models.AdvisoryMetadata{Name: advisory,
			Description: unknown, Synopsis: unknown, Summary: unknown, Solution: unknown}
		err := tx.Create(&item).Error
		if err != nil {
			return nil, err
		}
		createdAdvisoryIDs = append(createdAdvisoryIDs, item.ID)
		utils.Log("advisory", advisory).Info("unknown advisory created")
	}

	return &createdAdvisoryIDs, nil
}

func addNewSystemAdvisories(tx *gorm.DB, systemID int, advisoryIDs []int) error {
	advisoriesObjs := models.SystemAdvisoriesSlice{}
	for _, advisoryID := range advisoryIDs {
		advisoriesObjs = append(advisoriesObjs,
			models.SystemAdvisories{SystemID: systemID, AdvisoryID: advisoryID})
	}

	interfaceSlice := advisoriesObjs.ToInterfaceSlice()
	err := database.BulkInsert(tx, interfaceSlice)
	if err != nil {
		return err
	}
	return nil
}

func updateSystemAdvisories(tx *gorm.DB, systemID int, patched, unpatched, news []int) error {
	whenPatched := time.Now()
	err := updateSystemAdvisoriesWhenPatched(tx, systemID, patched, &whenPatched)
	if err != nil {
		return err
	}

	err = updateSystemAdvisoriesWhenPatched(tx, systemID, unpatched, nil)
	if err != nil {
		return err
	}

	err = addNewSystemAdvisories(tx, systemID, news)
	if err != nil {
		return err
	}
	return nil
}
