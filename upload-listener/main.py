from insights import run
from insights.parsers import yum_updateinfo as updateinfo

from kafka import KafkaConsumer, KafkaProducer

import os

KAFKA_ADDRESS = os.getenv("KAFKA_ADDRES", "kafka:9092")
KAFKA_GROUP = os.getenv("KAFKA_GROUP", "patchman")

UPLOAD_TOPIC = os.getenv("UPLOAD_TOPIC", "platform.upload.host-egress")
EVAL_TOPIC = os.getenv("EVAL_TOPIC", "patchman.evaluator.upload")

CONFIG = {
    'bootstrap_servers': KAFKA_ADDRESS,
    'group_id': KAFKA_GROUP,
    'enable_auto_commit': False
}

CONSUMER = KafkaConsumer(UPLOAD_TOPIC, **CONFIG)
PRODUCER = KafkaProducer(**CONFIG)


def parse_archive

if __name__ == '__main__':
    CONSUMER.subscribe([UPLOAD_TOPIC])
    for msg in CONSUMER:
        #TODO: Deploy to CI, dump messages
        print(msg)

