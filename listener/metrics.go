package listener

import (
	"app/base/utils"
	"app/manager/middlewares"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	EventUpload             = "upload"
	EventDelete             = "delete"
	ReceivedSuccess         = "success"
	ReceivedErrorIdentity   = "error-identity"
	ReceivedErrorParsing    = "error-parsing"
	ReceivedErrorProcessing = "error-processing"
	ReceivedErrorOtherType  = "error-other-type"
)

var (
	messagesReceivedCnt = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "patchman_engine",
		Subsystem: "listener",
		Name:      "kafka_message_received",
	}, []string{"event", "type"})

	handleDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "patchman_engine",
		Subsystem: "listener",
		Name:      "handle_duration_milliseconds",
		Buckets:   []float64{.1, 1, 10, 100, 1000, },
	}, []string{"event"})
)

func init() {
	prometheus.MustRegister(messagesReceivedCnt, handleDuration)
}

func RunMetrics() {
	// create web app
	app := gin.New()
	middlewares.Prometheus().Use(app)

	err := app.Run(":8081")
	if err != nil {
		utils.Log("err", err.Error()).Error()
		panic(err)
	}
}
