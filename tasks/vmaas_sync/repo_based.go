package vmaas_sync //nolint:revive,stylecheck

import (
	"app/base"
	"app/base/database"
	"app/base/mqueue"
	"app/base/utils"
	"app/base/vmaas"
	"app/tasks"
	"net/http"
	"time"
)

const LastEvalRepoBased = "last_eval_repo_based"
const LastSync = "last_sync"
const LastFullSync = "last_full_sync"
const VmaasExported = "vmaas_exported"

func getCurrentRepoBasedInventoryIDs() ([]mqueue.EvalData, error) {
	lastRepoBaseEval, err := database.GetTimestampKVValueStr(LastEvalRepoBased)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	redhatRepos, thirdPartyRepos, latestRepoChange, err := getUpdatedRepos(now, lastRepoBaseEval)
	if latestRepoChange == nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	allRepos := make([]string, 0, len(redhatRepos)+len(thirdPartyRepos))
	allRepos = append(allRepos, redhatRepos...)
	allRepos = append(allRepos, thirdPartyRepos...)

	inventoryAIDs, err := getRepoBasedInventoryIDs(allRepos)
	if err != nil {
		return nil, err
	}

	database.UpdateTimestampKVValue(LastEvalRepoBased, *latestRepoChange)

	return inventoryAIDs, nil
}

func getRepoBasedInventoryIDs(repos []string) ([]mqueue.EvalData, error) {
	var ids []mqueue.EvalData
	if len(repos) == 0 {
		return ids, nil
	}

	err := tasks.CancelableDB().Table("system_repo sr").
		Joins("JOIN repo ON repo.id = sr.repo_id").
		Joins("JOIN system_platform sp ON  sp.rh_account_id = sr.rh_account_id AND sp.id = sr.system_id").
		Joins("JOIN rh_account ra ON ra.id = sp.rh_account_id").
		Where("repo.name IN (?)", repos).
		Order("sp.rh_account_id").
		Select("distinct sp.inventory_id, sp.rh_account_id, ra.org_id").
		Scan(&ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// nolint: funlen
func getUpdatedRepos(syncStart time.Time, modifiedSince *string) ([]string, []string, *time.Time, error) {
	page := 1
	var reposRedHat []string
	var reposThirdParty []string
	var latestRepoChange *time.Time
	reposSyncStart := time.Now()
	for {
		reposReq := vmaas.ReposRequest{
			Page:           page,
			RepositoryList: []string{".*"},
			PageSize:       advisoryPageSize,
			ThirdParty:     utils.PtrBool(true),
			ModifiedSince:  modifiedSince,
		}

		vmaasCallFunc := func() (interface{}, *http.Response, error) {
			vmaasData := vmaas.ReposResponse{}
			resp, err := vmaasClient.Request(&base.Context, http.MethodPost, vmaasReposURL, &reposReq, &vmaasData)
			return &vmaasData, resp, err
		}

		vmaasDataPtr, err := utils.HTTPCallRetry(base.Context, vmaasCallFunc, vmaasCallExpRetry, vmaasCallMaxRetries)
		if err != nil {
			return nil, nil, nil, err
		}
		vmaasCallCnt.WithLabelValues("success").Inc()
		repos := vmaasDataPtr.(*vmaas.ReposResponse)
		if repos.Pages < 1 {
			utils.LogInfo("No repos returned from VMaaS")
			break
		}

		if repos.LatestRepoChange == nil {
			break
		}
		if latestRepoChange == nil || latestRepoChange.Before(*repos.LatestRepoChange.Time()) {
			// add 1 second to avoid re-evaluation of the latest repo
			// e.g. vmaas returns `2024-01-05T06:39:53.553807+00:00`
			// 		but patch stores to DB `2024-01-05T06:39:53Z`
			// 		then the next request to /repos is made with "modified_since": "2024-01-05T06:39:53Z"
			// 		which again returns repo modified at 2024-01-05T06:39:53.553807
			t := repos.LatestRepoChange.Time().Add(time.Second)
			latestRepoChange = &t
		}

		utils.LogInfo("page", page, "pages", repos.Pages, "count", len(repos.RepositoryList),
			"sync_duration", utils.SinceStr(syncStart, time.Second),
			"repos_sync_duration", utils.SinceStr(reposSyncStart, time.Second),
			"Downloaded repos")

		for k, contentSet := range repos.RepositoryList {
			thirdParty := false
			for _, repo := range contentSet {
				if repo["third_party"] == (interface{})(true) {
					thirdParty = true
				}
			}

			if thirdParty {
				reposThirdParty = append(reposThirdParty, k)
			} else {
				reposRedHat = append(reposRedHat, k)
			}
		}

		if page == repos.Pages {
			break
		}
		page++
	}

	utils.LogInfo("redhat", len(reposRedHat), "thirdparty", len(reposThirdParty), "Repos downloading complete")
	return reposRedHat, reposThirdParty, latestRepoChange, nil
}
