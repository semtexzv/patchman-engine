package listener

import (
	"app/base/utils"
	"context"
	"github.com/segmentio/kafka-go"
	"time"
)

var (
	uploadReader *kafka.Reader
)

func configure() {
	topic := utils.GetenvOrFail("UPLOAD_TOPIC")
	kafkaAddress := utils.GetenvOrFail("KAFKA_HOST")
	kafkaPort := utils.GetIntEnvOrFail("KAFKA_PORT")
	kafkaGroup := utils.GetenvOrFail("KAFKA_GROUP")

	utils.Log().Info("Connecting to ", kafkaAddress, ":", kafkaPort, " listening for ", topic)

	uploadReader = kafka.NewReader(kafka.ReaderConfig{
		Brokers:  []string{kafkaAddress + ":"+ string(kafkaPort)},
		Topic:    topic,
		GroupID:  kafkaGroup,
		MinBytes: 10e3, // 10KB
		MaxBytes: 10e6, // 10MB
	})

}

func shutdown() {
	err := uploadReader.Close()
	if err != nil {
		utils.Log("err", err.Error()).Error("unable to shutdown Kafka reader")
	}
}

func RunListener() {
	utils.Log().Info("listener starting")
	configure()
	defer shutdown()

	for {
		m, err := uploadReader.ReadMessage(context.Background())
		if err != nil {
			if err.Error() == "context deadline exceeded" {
				utils.Log().Info("waiting for messages")
				time.Sleep(time.Second)
				return
			}

			utils.Log("err", err.Error()).Error("unable to read message from Kafka reader")
			return
		}
		utils.Log().Info("Received message", string(m.Value))

	}
}
