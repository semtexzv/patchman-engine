package vmaas_sync

import (
	"app/base/database"
	"app/base/models"
	"app/base/mqueue"
	"context"
	"github.com/RedHatInsights/patchman-clients/vmaas"
	"github.com/antihax/optional"
	"time"
)

const TIMESTAMP_LAST_REPO_BASED_EVAL = "last_eval_repo_based"

func getLastRepoEval() (time.Time, error) {
	var kv models.TimestampKV
	tx := database.Db.Find(&kv)
	if tx.RowsAffected == 0 {
		return time.Time{}, nil
	}

	err := tx.Error
	if err != nil {
		return time.Time{}, err
	}

	return kv.Value, nil
}

func setLastRepobasedEval(time time.Time) error {
	kv := models.TimestampKV{
		Name:  TIMESTAMP_LAST_REPO_BASED_EVAL,
		Value: time,
	}

	return database.OnConflictUpdate(database.Db, "name", "value").Create(&kv).Error
}

func getUpdatedRepos() ([]string, error) {
	ctx := context.Background()
	when, err := getLastRepoEval()
	if err != nil {
		return nil, err
	}
	updatedRepos := []string{}
	page := 0
	pages := 1
	for page < pages {
		opts := vmaas.AppReposHandlerPostPostOpts{
			ReposRequest: optional.NewInterface(vmaas.ReposRequest{
				Page:           float32(page),
				PageSize:       1000,
				RepositoryList: []string{".*"},
				ModifiedSince:  when.String(),
			})}
		repos, _, err := vmaasClient.ReposApi.AppReposHandlerPostPost(ctx, &opts)
		if err != nil {
			return nil, err
		}
		pages = int(repos.Pages)
		page = int(repos.Page + 1)

		for k := range repos.RepositoryList {
			updatedRepos = append(updatedRepos, k)
		}
	}
	return updatedRepos, nil
}

func selectSystemsByRepo(repos []string) ([]string, error) {
	var systems []string
	repoQ := database.Db.Select("id").Table("repo").Where("name in ?", repos).SubQuery()
	sysRepoQ := database.Db.Select("distinct system_id").Table("system_repo").Where("repo_id in ?", repoQ)
	sysQ := database.Db.Select("inventory_id").Table("system_platform").Where("id in ?", sysRepoQ)

	err := sysQ.Scan(&systems).Error
	if err != nil {
		return nil, err
	}
	return systems, nil
}

func RepoBasedRecalc() error {
	ctx := context.Background()
	now := time.Now()
	updatedRepos, err := getUpdatedRepos()
	if err != nil {
		panic(err)
	}
	systems, err := selectSystemsByRepo(updatedRepos)
	if err != nil {
		panic(err)
	}
	writer := mqueue.WriterFromEnv("patchman.evaluator.recalc")
	for _, s := range systems {
		ev := mqueue.PlatformEvent{
			ID: s,
		}
		err = writer.WriteEvent(ctx, ev)
		if err != nil {
			panic(err)
		}
	}
	err = setLastRepobasedEval(now)
	if err != nil {
		panic(err)
	}
}
