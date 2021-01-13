package vmaas_sync //nolint:golint,stylecheck

import (
	"app/base"
	"app/base/core"
	"app/base/database"
	"app/base/models"
	"app/base/utils"
	"app/manager/middlewares"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"gorm.io/gorm"
	"time"
)

const (
	optOutOn          = "on"
	optOutOff         = "off"
	lastUploadLast1D  = "last1D"
	lastUploadLast7D  = "last7D"
	lastUploadLast30D = "last30D"
	lastUploadAll     = "all"
)

var (
	messagesReceivedCnt = prometheus.NewCounterVec(prometheus.CounterOpts{
		Help:      "How many websocket messages were received of which type",
		Namespace: "patchman_engine",
		Subsystem: "vmaas_sync",
		Name:      "websocket_msgs",
	}, []string{"type"})

	vmaasCallCnt = prometheus.NewCounterVec(prometheus.CounterOpts{
		Help:      "How many times vmaas was called with which result",
		Namespace: "patchman_engine",
		Subsystem: "vmaas_sync",
		Name:      "vmaas_call",
	}, []string{"type"})

	storeAdvisoriesCnt = prometheus.NewCounterVec(prometheus.CounterOpts{
		Help:      "How many advisories were loaded with which result",
		Namespace: "patchman_engine",
		Subsystem: "vmaas_sync",
		Name:      "store_advisories",
	}, []string{"type"})

	storePackagesCnt = prometheus.NewCounterVec(prometheus.CounterOpts{
		Help:      "How many packages were loaded with which result",
		Namespace: "patchman_engine",
		Subsystem: "vmaas_sync",
		Name:      "store_packages",
	}, []string{"type"})

	updateInterval = time.Second * 20

	systemsCnt = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Help:      "How many systems are stored and how up-to-date they are",
		Namespace: "patchman_engine",
		Subsystem: "vmaas_sync",
		Name:      "systems",
	}, []string{"opt_out", "last_upload"})

	advisoriesCnt = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Help:      "How many advisories are stored of which type",
		Namespace: "patchman_engine",
		Subsystem: "vmaas_sync",
		Name:      "advisories",
	}, []string{"type"})

	systemAdvisoriesStats = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Help:      "Max advisories per system found of which type",
		Namespace: "patchman_engine",
		Subsystem: "vmaas_sync",
		Name:      "system_advisories_stats",
	}, []string{"type"})

	syncDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Help:      "How long it took to sync from vmaas service",
		Namespace: "patchman_engine",
		Subsystem: "vmaas_sync",
		Name:      "sync_duration_seconds",
	})

	messageSendDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Help:      "How long it took to send message",
		Namespace: "patchman_engine",
		Subsystem: "vmaas_sync",
		Name:      "message_send_duration_seconds",
	})

	deletedCulledSystemsCnt = prometheus.NewCounter(prometheus.CounterOpts{
		Help:      "How many culled systems were deleted",
		Namespace: "patchman_engine",
		Subsystem: "vmaas_sync",
		Name:      "deleted_culled_systems",
	})

	staleSystemsMarkedCnt = prometheus.NewCounter(prometheus.CounterOpts{
		Help:      "How many systems were marked as stale",
		Namespace: "patchman_engine",
		Subsystem: "vmaas_sync",
		Name:      "stale_systems_marked",
	})

	enableCyndiMetrics = utils.GetBoolEnvOrDefault("ENABLE_CYNDI_METRICS", true)
)

func RunMetrics() {
	prometheus.MustRegister(messagesReceivedCnt, vmaasCallCnt, storeAdvisoriesCnt, storePackagesCnt,
		systemsCnt, advisoriesCnt, systemAdvisoriesStats, syncDuration, messageSendDuration,
		deletedCulledSystemsCnt, staleSystemsMarkedCnt,
		databaseSizeBytesGaugeVec, databaseProcessesGaugeVec, cyndiSystemsCnt, cyndiTagsCnt)

	go runAdvancedMetricsUpdating()

	// create web app
	app := gin.New()
	core.InitProbes(app)
	middlewares.Prometheus().Use(app)
	err := utils.RunServer(base.Context, app, ":8083")
	if err != nil {
		utils.Log("err", err.Error()).Error()
		panic(err)
	}
}

func runAdvancedMetricsUpdating() {
	defer utils.LogPanics(true)

	utils.Log().Info("started advanced metrics updating")
	for {
		update()
		time.Sleep(updateInterval)
	}
}

func update() {
	updateSystemMetrics()
	updateAdvisoryMetrics()
	updateSystemAdvisoriesStats()
	updateDBMetrics()

	if enableCyndiMetrics {
		updateCyndiData()
		updateCyndiSystemMetrics()
	}
}

func updateSystemMetrics() {
	counts, err := getSystemCounts(time.Now())
	if err != nil {
		utils.Log("err", err.Error()).Error("unable to update system metrics")
		return
	}

	for labels, count := range counts {
		systemsCnt.WithLabelValues(labels.OptOut, labels.LastUpload).Set(float64(count))
	}
}

type systemsCntLabels struct {
	OptOut     string
	LastUpload string
}

// Load stored systems counts according to "opt_out" and "last_upload" properties.
// Result is loaded into the map {"opt_out_on:last1D": 12, "opt_out_off:last1D": 3, ...}.
func getSystemCounts(refTime time.Time) (map[systemsCntLabels]int, error) {
	systemsQuery := database.Db.Model(&models.SystemPlatform{})
	optOutKV := map[string]bool{optOutOn: true, optOutOff: false}
	lastUploadKV := map[string]int{lastUploadLast1D: 1, lastUploadLast7D: 7, lastUploadLast30D: 30, lastUploadAll: -1}
	counts := map[systemsCntLabels]int{}
	for optOutK, optOutV := range optOutKV {
		systemsQueryOptOut := updateSystemsQueryOptOut(systemsQuery, optOutV)
		for lastUploadK, lastUploadV := range lastUploadKV {
			systemsQueryOptOutLastUpload := updateSystemsQueryLastUpload(systemsQueryOptOut, refTime, lastUploadV)
			var nSystems int64
			err := systemsQueryOptOutLastUpload.Count(&nSystems).Error
			if err != nil {
				return nil, errors.Wrap(err, "unable to load systems counts: "+
					fmt.Sprintf("opt_out: %v, last_upload_before_days: %v", optOutV, lastUploadV))
			}
			counts[systemsCntLabels{optOutK, lastUploadK}] = int(nSystems)
		}
	}
	return counts, nil
}

// Update input systems query with "opt_out = X" constraint.
func updateSystemsQueryOptOut(systemsQuery *gorm.DB, optOut bool) *gorm.DB {
	return systemsQuery.Where("opt_out = ?", optOut)
}

// Update input systems query with "last_upload > T" constraint.
// Constraint is not added if "lastNDays" argument is negative.
func updateSystemsQueryLastUpload(systemsQuery *gorm.DB, refTime time.Time, lastNDays int) *gorm.DB {
	if lastNDays >= 0 {
		return systemsQuery.Where("last_upload > ?", refTime.AddDate(0, 0, -lastNDays))
	}
	return systemsQuery
}

func updateAdvisoryMetrics() {
	unknown, enh, bug, sec, err := getAdvisoryCounts()
	if err != nil {
		utils.Log("err", err.Error()).Error("unable to update advisory metrics")
	}
	advisoriesCnt.WithLabelValues("unknown").Set(float64(unknown))
	advisoriesCnt.WithLabelValues("enhancement").Set(float64(enh))
	advisoriesCnt.WithLabelValues("bugfix").Set(float64(bug))
	advisoriesCnt.WithLabelValues("security").Set(float64(sec))
}

func getAdvisoryCounts() (unknown, enh, bug, sec int64, err error) {
	advisoryQuery := database.Db.Model(&models.AdvisoryMetadata{})
	err = advisoryQuery.Where("advisory_type_id = 0").Count(&unknown).Error
	if err != nil {
		return 0, 0, 0, 0, errors.Wrap(err, "unable to get advisories count - type unknown")
	}

	err = advisoryQuery.Where("advisory_type_id = 1").Count(&enh).Error
	if err != nil {
		return 0, 0, 0, 0, errors.Wrap(err, "unable to get advisories count - type enhancement")
	}

	err = advisoryQuery.Where("advisory_type_id = 2").Count(&bug).Error
	if err != nil {
		return 0, 0, 0, 0, errors.Wrap(err, "unable to get advisories count - type bugfix")
	}

	err = advisoryQuery.Where("advisory_type_id = 3").Count(&sec).Error
	if err != nil {
		return 0, 0, 0, 0, errors.Wrap(err, "unable to get advisories count - type security")
	}
	return unknown, enh, bug, sec, nil
}

func updateSystemAdvisoriesStats() {
	stats, err := getSystemAdvisorieStats()
	if err != nil {
		utils.Log("err", err.Error()).Info()
		stats = SystemAdvisoryStats{}
	}
	systemAdvisoriesStats.WithLabelValues("max_all").Set(float64(stats.MaxAll))
	systemAdvisoriesStats.WithLabelValues("max_enh").Set(float64(stats.MaxEnh))
	systemAdvisoriesStats.WithLabelValues("max_bug").Set(float64(stats.MaxBug))
	systemAdvisoriesStats.WithLabelValues("max_sec").Set(float64(stats.MaxSec))
}

type SystemAdvisoryStats struct {
	MaxAll int
	MaxEnh int
	MaxBug int
	MaxSec int
}

func getSystemAdvisorieStats() (stats SystemAdvisoryStats, err error) {
	err = database.Db.Table("system_platform").
		Select("MAX(advisory_count_cache) as max_all, MAX(advisory_enh_count_cache) as max_enh," +
			"MAX(advisory_bug_count_cache) as max_bug, MAX(advisory_sec_count_cache) as max_sec").
		First(&stats).Error
	if err != nil {
		return stats, errors.Wrap(err, "unable to get system advisory stats from db")
	}
	return stats, nil
}
